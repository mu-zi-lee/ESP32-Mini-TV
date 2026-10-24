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
// A frame may contain many UDP chunks and the Mac can occasionally spend
// longer than one capture interval encoding it. Keep the last good image on
// screen during that short gap instead of flashing NO SIGNAL.
constexpr uint32_t kFrameTimeoutMs = 1200;
Display* activeDisplay = nullptr;

uint16_t readU16(const uint8_t* p) { return static_cast<uint16_t>(p[0] | (p[1] << 8)); }
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
}

void MirrorReceiver::stop() {
  udp_.stop();
  active_ = false;
  haveFrame_ = false;
  modes_.setMirrorActive(false);
}

void MirrorReceiver::handle() {
  int packetSize = udp_.parsePacket();
  if (packetSize <= 0) {
    if (active_ && millis() - lastPacketAt_ > kFrameTimeoutMs) {
      active_ = false;
      modes_.setMirrorActive(false);
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
    if (length > 0) acceptPacket(packet, static_cast<size_t>(length));
    packetSize = udp_.parsePacket();
  }
  if (receivedChunks_ > 0 && receivedChunks_ >= chunkCount_) finishFrame();
}

bool MirrorReceiver::acceptPacket(const uint8_t* packet, size_t length) {
  if (length < kHeaderSize || packet[0] != kMagic0 || packet[1] != kMagic1 || packet[2] != kVersion) return false;
  const uint16_t frameId = readU16(packet + 3);
  const uint8_t width = packet[5];
  const uint8_t height = packet[6];
  const uint16_t chunk = readU16(packet + 7);
  const uint16_t chunks = readU16(packet + 9);
  const uint16_t payload = readU16(packet + 11);
  if (width != 160 || height != 128 || chunks == 0 || chunk >= chunks || payload == 0 ||
      payload + kHeaderSize != length || chunks > 64 || payload > kChunkPayload) return false;

  // Drop delayed packets from older frames so network jitter cannot build up
  // visible latency. Sequence arithmetic also handles the uint16 wraparound.
	// A sender starts a fresh sequence when a mirror session is restarted or
	// when the Control app switches from a physical capture to the virtual
	// display. Treat the first packet after a receive timeout as a new epoch so
	// frame IDs are allowed to restart at zero.
	const bool newer = !active_ || !haveFrame_ || static_cast<int16_t>(frameId - frameId_) > 0;
  if (newer) {
    frameId_ = frameId;
    haveFrame_ = true;
    chunkCount_ = chunks;
    receivedChunks_ = 0;
    receivedBitmap_ = 0;
    frameLength_ = 0;
  } else if (frameId != frameId_ || chunks != chunkCount_) {
    return false;
  }
  const uint32_t offset = static_cast<uint32_t>(chunk) * kChunkPayload;
  if (offset + payload > sizeof(frame_)) return false;
  memcpy(frame_ + offset, packet + kHeaderSize, payload);
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
  if (TJpgDec.drawJpg(0, 0, frame_, frameLength_) != JDR_OK) return false;
  modes_.recordMirrorFrame();
  receivedChunks_ = 0;
  receivedBitmap_ = 0;
  frameLength_ = 0;
  return true;
}

}  // namespace mini_tv
