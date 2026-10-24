#include "MirrorReceiver.h"

#include <TJpg_Decoder.h>
#include <string.h>

namespace mini_tv {
namespace {
constexpr uint8_t kMagic0 = 'M';
constexpr uint8_t kMagic1 = 'T';
constexpr uint8_t kVersion = 1;
constexpr size_t kHeaderSize = 14;
constexpr uint16_t kChunkPayload = 1186;
constexpr size_t kTileHeaderSize = 18;
constexpr uint16_t kTileChunkPayload = 1182;
constexpr uint8_t kModeJpeg = 0;
constexpr uint8_t kModeTile = 1;
constexpr uint8_t kModeRawRgb565 = 2;
// A frame may contain many UDP chunks and the Mac can occasionally spend
// longer than one capture interval encoding it. Keep the last good image on
// screen during that short gap instead of flashing NO SIGNAL.
// A complete raw frame is ~41KB and may take several hundred milliseconds on
// a busy Wi-Fi link. Keep the last valid frame visible during short gaps; only
// the explicit HTTP stop command should paint the NO SIGNAL placeholder.
constexpr uint32_t kFrameTimeoutMs = 5000;
Display* activeDisplay = nullptr;

uint16_t readU16(const uint8_t* p) { return static_cast<uint16_t>(p[0] | (p[1] << 8)); }
uint32_t frameHash(const uint8_t* data, size_t length) {
  uint32_t hash = 2166136261u;
  for (size_t index = 0; index < length; ++index) {
    hash ^= data[index];
    hash *= 16777619u;
  }
  return hash;
}
bool drawJpeg(int16_t x, int16_t y, uint16_t width, uint16_t height, uint16_t* bitmap) {
  if (!activeDisplay || y >= activeDisplay->canvas().height()) return false;
  activeDisplay->canvas().drawRGBBitmap(x, y, bitmap, width, height);
  return true;
}
}  // namespace

void MirrorReceiver::begin() {
  activeDisplay = &display_;
  TJpgDec.setJpgScale(1);
  TJpgDec.setCallback(drawJpeg);
  udp_.begin(port_);
  active_ = false;
  haveRenderedHash_ = false;
  renderedHash_ = 0;
}

void MirrorReceiver::stop() {
  udp_.stop();
  active_ = false;
  haveFrame_ = false;
  haveRenderedHash_ = false;
  renderedHash_ = 0;
  modes_.setMirrorActive(false);
}

void MirrorReceiver::handle() {
  int packetSize = udp_.parsePacket();
  if (packetSize <= 0) {
    if (active_ && millis() - lastPacketAt_ > kFrameTimeoutMs) {
      active_ = false;
      modes_.mirrorLinkLost();
    }
    return;
  }
  uint8_t packet[kChunkPayload + kHeaderSize];
  while (packetSize > 0) {
    const int length = udp_.read(packet, sizeof(packet));
    // Keep draining packets before decoding. A JPEG draw is synchronous and
    // can take several milliseconds; decoding immediately on the final chunk
    // of an old frame would otherwise let the next frame overflow the UDP
    // receive queue. acceptPacket() always keeps the newest sequence number.
    (void)length;
    if (length > 0) {
      acceptPacket(packet, static_cast<size_t>(length));
      if (receivedChunks_ > 0 && receivedChunks_ >= chunkCount_) {
        if (packetMode_ == 1) finishTile();
        else finishFrame();
      }
    }
    packetSize = udp_.parsePacket();
  }
}

bool MirrorReceiver::acceptPacket(const uint8_t* packet, size_t length) {
  if (length < kHeaderSize || packet[0] != kMagic0 || packet[1] != kMagic1 || packet[2] != kVersion) return false;
  const uint8_t mode = packet[13];
  if (mode > kModeRawRgb565) return false;
  const size_t headerSize = mode == kModeTile ? kTileHeaderSize : kHeaderSize;
  if (length < headerSize) return false;
  const uint16_t frameId = readU16(packet + 3);
  const uint8_t width = packet[5];
  const uint8_t height = packet[6];
  const uint16_t chunk = readU16(packet + 7);
  const uint16_t chunks = readU16(packet + 9);
  const uint16_t payload = readU16(packet + 11);
  if (width != 160 || height != 128 || chunks == 0 || chunk >= chunks || payload == 0 ||
      payload + headerSize != length || chunks > 64 ||
      payload > (mode == kModeTile ? kTileChunkPayload : kChunkPayload)) return false;
  if (mode == kModeRawRgb565 && chunks != 35) return false;
  uint8_t tileX = 0;
  uint8_t tileY = 0;
  uint8_t tileWidth = 0;
  uint8_t tileHeight = 0;
  if (mode == kModeTile) {
    tileX = packet[14];
    tileY = packet[15];
    tileWidth = packet[16];
    tileHeight = packet[17];
    if (tileWidth == 0 || tileHeight == 0 || tileWidth > 32 || tileHeight > 32 ||
        tileX + tileWidth > 160 || tileY + tileHeight > 128) return false;
  }

  // Drop delayed packets from older frames so network jitter cannot build up
  // visible latency. Sequence arithmetic also handles the uint16 wraparound.
	// A sender starts a fresh sequence when a mirror session is restarted or
	// when the Control app switches from a physical capture to the virtual
	// display. Treat the first packet after a receive timeout as a new epoch so
	// frame IDs are allowed to restart at zero.
  const bool newer = !active_ || !haveFrame_ || static_cast<int16_t>(frameId - frameId_) > 0;
  if (newer) {
    if (!active_) {
      // A new mirror session may start with the same JPEG as the previous
      // session, but another UI page could have painted the panel meanwhile.
      // Force that first frame to redraw instead of treating it as a heartbeat.
      haveRenderedHash_ = false;
    }
    frameId_ = frameId;
    haveFrame_ = true;
    chunkCount_ = chunks;
    receivedChunks_ = 0;
    receivedBitmap_ = 0;
    frameLength_ = 0;
    packetMode_ = mode;
    tileX_ = tileX;
    tileY_ = tileY;
    tileWidth_ = tileWidth;
    tileHeight_ = tileHeight;
  } else if (frameId != frameId_) {
    return false;
  } else if (mode == kModeJpeg || mode == kModeRawRgb565) {
    if (packetMode_ != mode || chunks != chunkCount_) return false;
  } else if (packetMode_ != kModeTile) {
    return false;
  } else if (receivedChunks_ == 0) {
    // One frame ID can carry several independent tiles. The sender emits a
    // tile's chunks consecutively, so begin the next tile after the previous
    // one has been decoded and cleared below.
    chunkCount_ = chunks;
    frameLength_ = 0;
    receivedBitmap_ = 0;
    tileX_ = tileX;
    tileY_ = tileY;
    tileWidth_ = tileWidth;
    tileHeight_ = tileHeight;
  } else if (chunks != chunkCount_ || tileX != tileX_ || tileY != tileY_ ||
             tileWidth != tileWidth_ || tileHeight != tileHeight_) {
    return false;
  }
  const uint16_t chunkPayload = mode == kModeTile ? kTileChunkPayload : kChunkPayload;
  const size_t packetHeader = mode == kModeTile ? kTileHeaderSize : kHeaderSize;
  const uint32_t offset = static_cast<uint32_t>(chunk) * chunkPayload;
  if (mode == kModeRawRgb565) {
    if (offset + payload > sizeof(buffers_.rawFrame)) return false;
    memcpy(buffers_.rawFrame + offset, packet + packetHeader, payload);
  } else {
    if (offset + payload > sizeof(buffers_.frame)) return false;
    memcpy(buffers_.frame + offset, packet + packetHeader, payload);
  }
  frameLength_ = max<uint16_t>(frameLength_, static_cast<uint16_t>(offset + payload));
  const uint64_t bit = 1ULL << chunk;
  if ((receivedBitmap_ & bit) == 0) {
    receivedBitmap_ |= bit;
    receivedChunks_++;
  }
  lastPacketAt_ = millis();
  active_ = true;
  return receivedChunks_ >= chunkCount_;
}

bool MirrorReceiver::finishFrame() {
  if (frameLength_ == 0) return false;
  if (packetMode_ == kModeRawRgb565) {
    display_.canvas().startWrite();
    display_.canvas().setAddrWindow(0, 0, 160, 128);
    display_.canvas().writePixels(reinterpret_cast<uint16_t*>(buffers_.rawFrame), 160 * 128, true, true);
    display_.canvas().endWrite();
    modes_.recordMirrorFrame();
    receivedChunks_ = 0;
    receivedBitmap_ = 0;
    frameLength_ = 0;
    return true;
  }
  const uint32_t hash = frameHash(buffers_.frame, frameLength_);
  // Heartbeats reuse the last JPEG so the ESP32 remains online while the Mac
  // desktop is idle. Do not pay the ~60 ms decode + full SPI flush cost again
  // when the pixels cannot have changed.
  if (!haveRenderedHash_ || hash != renderedHash_) {
    if (TJpgDec.drawJpg(0, 0, buffers_.frame, frameLength_) != JDR_OK) return false;
    renderedHash_ = hash;
    haveRenderedHash_ = true;
  }
  modes_.recordMirrorFrame();
  receivedChunks_ = 0;
  receivedBitmap_ = 0;
  frameLength_ = 0;
  return true;
}

bool MirrorReceiver::finishTile() {
  if (frameLength_ == 0 || packetMode_ != kModeTile) return false;
  const int result = TJpgDec.drawJpg(tileX_, tileY_, buffers_.frame, frameLength_);
  if (result != JDR_OK) return false;
  modes_.recordMirrorFrame();
  receivedChunks_ = 0;
  receivedBitmap_ = 0;
  frameLength_ = 0;
  return true;
}

}  // namespace mini_tv
