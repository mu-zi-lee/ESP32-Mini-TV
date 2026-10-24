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
  uint32_t renderedHash_ = 0;
  bool haveRenderedHash_ = false;
  uint32_t lastPacketAt_ = 0;
  uint16_t port_ = 8765;
  bool active_ = false;
};

}  // namespace mini_tv
