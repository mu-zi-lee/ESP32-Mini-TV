#pragma once

#include <Arduino.h>

namespace mini_tv {

enum class Mode : uint8_t { Home, Mirror, Music, Dashboard, Settings, Error };
enum class NetworkState : uint8_t { Offline, Connecting, Online };

struct AppState {
  Mode mode = Mode::Home;
  NetworkState network = NetworkState::Offline;
  bool mockData = true;
  bool telemetryValid = false;
  bool batteryCharging = false;
  bool musicPlaying = false;
  bool mirrorActive = false;
  uint8_t cpuPercent = 0;
  uint8_t memoryPercent = 0;
  uint8_t batteryPercent = 0;
  uint16_t musicPosition = 0;
  uint16_t musicDuration = 0;
  uint16_t mirrorFrames = 0;
  uint32_t frameCount = 0;
  uint32_t startedAt = 0;
  uint32_t lastTelemetryAt = 0;
  uint32_t lastMirrorFrameAt = 0;
  char track[48] = "";
  char artist[40] = "";
  const char* errorMessage = "No error";
};

const char* modeName(Mode mode);
const char* networkName(NetworkState state);

}  // namespace mini_tv
