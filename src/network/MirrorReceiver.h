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

  ModeManager& modes_;
  Display& display_;
  WiFiUDP udp_;
  static constexpr size_t FrameBufferSize = 48 * 1024;
  uint8_t frame_[FrameBufferSize]{};
  uint16_t frameId_ = 0;
  bool haveFrame_ = false;
  uint16_t chunkCount_ = 0;
  uint16_t receivedChunks_ = 0;
  uint64_t receivedBitmap_ = 0;
  uint16_t frameLength_ = 0;
  uint32_t lastPacketAt_ = 0;
  uint16_t port_ = 8765;
  bool active_ = false;
};

}  // namespace mini_tv
