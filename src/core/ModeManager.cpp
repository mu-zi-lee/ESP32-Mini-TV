#include "ModeManager.h"

#include <string.h>
#include "../ui/screens/Screens.h"

namespace mini_tv {

void ModeManager::begin() { state_.startedAt = millis(); render(); }

void ModeManager::setMode(Mode mode) {
  // A live mirror owns the whole panel. Keep the selected mode on MIRROR
  // until the sender is explicitly stopped, otherwise periodic telemetry and
  // the main loop can paint HOME over incoming frames.
  if (state_.mirrorActive && mode != Mode::Mirror) return;
  if (state_.mode == mode) return;
  state_.mode = mode;
  state_.frameCount++;
  // The companion marks mirror active before changing modes. Keep the first
  // received frame on screen instead of flashing the placeholder card again.
  if (mode == Mode::Mirror && state_.mirrorActive) return;
  render();
}

void ModeManager::next() {
  // The device now has two intentional user-facing pages: overview and
  // mirror. Music/system remain accepted as legacy aliases for overview.
  const uint8_t nextMode = state_.mode == Mode::Home ? static_cast<uint8_t>(Mode::Mirror)
                                                     : static_cast<uint8_t>(Mode::Home);
  setMode(static_cast<Mode>(nextMode));
}

bool ModeManager::setModeByName(const char* name) {
  if (!strcmp(name, "home")) setMode(Mode::Home);
  else if (!strcmp(name, "mirror")) setMode(Mode::Mirror);
  else if (!strcmp(name, "music") || !strcmp(name, "dashboard") || !strcmp(name, "dash")) {
    // Preserve compatibility with older Control builds while avoiding a
    // duplicate page that overlaps the home overview.
    setMode(Mode::Home);
  }
  else if (!strcmp(name, "settings")) setMode(Mode::Settings);
  else if (!strcmp(name, "error")) setMode(Mode::Error);
  else return false;
  return true;
}

void ModeManager::updateTelemetry(uint8_t cpu, uint8_t memory, uint8_t battery, bool charging,
                                  bool playing, const char* track, const char* artist,
                                  uint16_t position, uint16_t duration,
                                  const char* localTime, const char* localDate) {
  state_.cpuPercent = cpu > 100 ? 100 : cpu;
  state_.memoryPercent = memory > 100 ? 100 : memory;
  state_.batteryPercent = battery > 100 ? 100 : battery;
  state_.batteryCharging = charging;
  state_.musicPlaying = playing;
  state_.musicPosition = position;
  state_.musicDuration = duration;
  snprintf(state_.localTime, sizeof(state_.localTime), "%s", localTime ? localTime : "--:--");
  snprintf(state_.localDate, sizeof(state_.localDate), "%s", localDate ? localDate : "NO DATE");
  snprintf(state_.track, sizeof(state_.track), "%s", track ? track : "");
  snprintf(state_.artist, sizeof(state_.artist), "%s", artist ? artist : "");
  state_.telemetryValid = true;
  state_.mockData = false;
  state_.lastTelemetryAt = millis();
  // Control owns mode changes. Telemetry refreshes the selected page without
  // interrupting the user's current view.
  if (state_.mode == Mode::Dashboard) {
    screens::renderDashboardDynamic(display_, state_);
  } else if (state_.mode == Mode::Music) {
    screens::renderMusicDynamic(display_, state_);
  } else if (state_.mode == Mode::Home) {
    screens::renderHomeDynamic(display_, state_);
  }
}

void ModeManager::setMirrorActive(bool active) {
  if (state_.mirrorActive == active) return;
  state_.mirrorActive = active;
  if (active && state_.mode != Mode::Mirror) {
    state_.mode = Mode::Mirror;
    state_.frameCount++;
  }
  if (state_.mode == Mode::Mirror && !active) render();
}

void ModeManager::mirrorLinkLost() {
  // Keep mirrorActive true after a receive gap. This prevents a transient UDP
  // loss from painting the NO SIGNAL placeholder over the last good frame.
  if (state_.mode == Mode::Mirror && state_.mirrorFrames > 0) {
    state_.mirrorActive = true;
  }
}

void ModeManager::recordMirrorFrame() {
  if (!state_.mirrorActive) return;
  state_.mirrorFrames++;
  state_.lastMirrorFrameAt = millis();
}

void ModeManager::render() {
  // A received mirror frame owns the whole 160x128 canvas. Network state
  // changes can request a general render, but must not paint the NO SIGNAL
  // placeholder over a live frame.
  if (state_.mirrorActive) return;
  switch (state_.mode) {
    case Mode::Home: screens::renderHome(display_, state_); break;
    case Mode::Mirror: screens::renderMirror(display_, state_); break;
    case Mode::Music: screens::renderMusic(display_, state_); break;
    case Mode::Dashboard: screens::renderDashboard(display_, state_); break;
    case Mode::Settings: screens::renderSettings(display_, state_); break;
    case Mode::Error: screens::renderError(display_, state_); break;
  }
}

}  // namespace mini_tv
