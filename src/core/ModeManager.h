#pragma once

#include "Model.h"
#include "../display/Display.h"

namespace mini_tv {

class ModeManager {
 public:
  explicit ModeManager(Display& display) : display_(display) {}
  void begin();
  void render();
  void setMode(Mode mode);
  void next();
  bool setModeByName(const char* name);
  void setNetworkState(NetworkState state) { state_.network = state; }
  void updateTelemetry(uint8_t cpu, uint8_t memory, uint8_t battery, bool charging,
                      bool playing, const char* track, const char* artist,
                      uint16_t position, uint16_t duration);
  void setMirrorActive(bool active);
  // A transport gap is different from an explicit user stop: keep the last
  // rendered mirror frame on screen while the sender is recovering.
  void mirrorLinkLost();
  void recordMirrorFrame();
  const AppState& state() const { return state_; }

 private:
  Display& display_;
  AppState state_;
};

}  // namespace mini_tv
