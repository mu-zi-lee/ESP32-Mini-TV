#pragma once

#include <WiFi.h>
#include <WiFiUdp.h>

#include "../core/ModeManager.h"
#include "../display/Display.h"

namespace mini_tv {

class MirrorReceiver {
 public:
  MirrorReceiver(ModeManager& modes, Display& display) : modes_(modes), display_(display) {}
  void begin();
  void handle();
  void stop();

 private:
  bool acceptPacket(const uint8_t* packet, size_t length);
  bool finishFrame();
  bool finishTile();
  void setAgentStatus(uint8_t status);
  void renderAgentStatus(bool force = false);

  ModeManager& modes_;
  Display& display_;
  WiFiUDP udp_;
  static constexpr size_t FrameBufferSize = 48 * 1024;
  static constexpr size_t RawFrameSize = 160 * 128 * 2;
  // JPEG and raw RGB565 frames are never received at the same time. Overlay
  // their storage so the raw mode does not consume another 40KB of DRAM.
  alignas(4) union {
    uint8_t frame[FrameBufferSize];
    uint8_t rawFrame[RawFrameSize];
  } buffers_{};
  // One off-screen canvas is enough because an incomplete tile batch is never
  // displayed. After a failed batch, wait for the next 20-tile keyframe.
  // RGB332 keeps the transactional canvas at 20KB instead of 40KB. It is
  // expanded to RGB565 one scanline at a time when the batch is committed.
  alignas(4) uint8_t tileCanvas_[160 * 128]{};
  uint16_t frameId_ = 0;
  bool haveFrame_ = false;
  uint16_t chunkCount_ = 0;
  uint16_t receivedChunks_ = 0;
  uint64_t receivedBitmap_ = 0;
  uint16_t frameLength_ = 0;
  uint8_t packetMode_ = 0;
  uint8_t tileX_ = 0;
  uint8_t tileY_ = 0;
  uint8_t tileWidth_ = 0;
  uint8_t tileHeight_ = 0;
  uint16_t tileIndex_ = 0;
  uint16_t tileBatchCount_ = 0;
  uint32_t receivedTileBitmap_ = 0;
  bool tileBatchNeedsKeyframe_ = false;
  bool tileBatchAccepted_ = true;
  uint8_t tileRects_[20][4]{};
  uint32_t renderedHash_ = 0;
  bool haveRenderedHash_ = false;
  uint32_t lastPacketAt_ = 0;
  uint16_t port_ = 8765;
  bool active_ = false;
  bool udpBound_ = false;
  bool lastWifiConnected_ = false;
  uint32_t packetsSeen_ = 0;
  uint32_t packetsRejected_ = 0;
  uint32_t framesFailed_ = 0;
  uint8_t agentStatus_ = 0;
  uint8_t renderedAgentStatus_ = 255;
  bool renderedAgentVisible_ = false;
  bool agentOverlayPainted_ = false;

 public:
  bool udpBound() const { return udpBound_; }
  uint32_t packetsSeen() const { return packetsSeen_; }
  uint32_t packetsRejected() const { return packetsRejected_; }
  uint32_t framesFailed() const { return framesFailed_; }
};

}  // namespace mini_tv
