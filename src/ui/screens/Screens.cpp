#include "Screens.h"

#include <Arduino.h>
#include <string.h>

#include "../components/UiPrimitives.h"

namespace mini_tv::screens {
using ui::header;
using ui::progressBar;
using ui::text;

namespace {
struct HomeRenderCache {
  bool initialized = false;
  int cpu = -2;
  int memory = -2;
  int battery = -2;
  bool wifiOnline = false;
  bool macOnline = false;
  bool hasMusic = false;
  char localTime[6] = "";
  char localDate[12] = "";
  char music[64] = "";
};

HomeRenderCache homeCache;

bool telemetryFresh(const AppState& state) {
  return state.telemetryValid && millis() - state.lastTelemetryAt < 10000UL;
}

const char* musicLabel(const char* value, const char* fallback) {
  static char filtered[64];
  if (!value || value[0] == '\0') return fallback;
  size_t out = 0;
  bool sawText = false;
  for (const uint8_t* cursor = reinterpret_cast<const uint8_t*>(value);
       *cursor && out + 1 < sizeof(filtered); ++cursor) {
    if (*cursor >= 0x20 && *cursor < 0x7F) {
      filtered[out++] = static_cast<char>(*cursor);
      if (*cursor != ' ') sawText = true;
    } else if (out > 0 && filtered[out - 1] != ' ') {
      filtered[out++] = ' ';
    }
  }
  while (out > 0 && filtered[out - 1] == ' ') --out;
  filtered[out] = '\0';
  return sawText ? filtered : fallback;
}

void metric(Adafruit_GFX& tft, int16_t x, int16_t y, const char* label, int value, uint16_t color) {
  char buffer[8];
  if (value < 0) snprintf(buffer, sizeof(buffer), "--");
  else snprintf(buffer, sizeof(buffer), "%u%%", static_cast<unsigned>(min(value, 100)));
  text(tft, x, y, label, 1, ui::theme().muted);
  text(tft, x + 33, y, buffer, 1, color);
}

void clearHomeRegion(Adafruit_GFX& tft, int16_t x, int16_t y, int16_t width, int16_t height) {
  tft.fillRect(x, y, width, height, ui::theme().background);
}

}  // namespace

void renderHome(Display& display, const AppState& state) {
  homeCache.initialized = false;
  ui::clear(display.canvas());
  header(display.canvas(), "HOME", state);
  renderHomeDynamic(display, state);
}

void renderHomeDynamic(Display& display, const AppState& state) {
  auto& tft = display.canvas();
  const bool fresh = telemetryFresh(state);
  const bool hasMusic = fresh && state.track[0] != '\0';
  const char* localTime = fresh ? state.localTime : "--:--";
  const char* localDate = fresh ? state.localDate : "NO DATE";
  const char* music = hasMusic ? musicLabel(state.track, "PLAYING") : "";
  const int cpu = fresh ? state.cpuPercent : -1;
  const int memory = fresh ? state.memoryPercent : -1;
  const int battery = fresh ? state.batteryPercent : -1;
  const bool wifiOnline = state.network == NetworkState::Online;
  const bool macOnline = fresh;

  // Keep a cache of what is already on the panel. A steady field must not
  // trigger a SPI write at all; only its own small rectangle is refreshed
  // when the value changes.
  if (!homeCache.initialized || strcmp(homeCache.localTime, localTime) != 0) {
    clearHomeRegion(tft, 4, 21, 100, 29);
    text(tft, 7, 25, localTime, 3, fresh ? ui::theme().text : ui::theme().muted);
    snprintf(homeCache.localTime, sizeof(homeCache.localTime), "%s", localTime);
  }
  if (!homeCache.initialized || strcmp(homeCache.localDate, localDate) != 0) {
    clearHomeRegion(tft, 5, 51, 100, 13);
    text(tft, 9, 54, localDate, 1, ui::theme().muted);
    snprintf(homeCache.localDate, sizeof(homeCache.localDate), "%s", localDate);
  }
  if (!homeCache.initialized || homeCache.cpu != cpu) {
    clearHomeRegion(tft, 5, 69, 73, 14);
    metric(tft, 8, 72, "CPU", cpu, ui::theme().accent);
    homeCache.cpu = cpu;
  }
  if (!homeCache.initialized || homeCache.memory != memory) {
    clearHomeRegion(tft, 83, 69, 73, 14);
    metric(tft, 86, 72, "MEM", memory, ui::theme().accent);
    homeCache.memory = memory;
  }
  if (!homeCache.initialized || homeCache.wifiOnline != wifiOnline || homeCache.macOnline != macOnline) {
    clearHomeRegion(tft, 5, 86, 150, 14);
    text(tft, 8, 89, wifiOnline ? "WIFI GOOD" : "WIFI WAIT", 1,
         wifiOnline ? ui::theme().good : ui::theme().warning);
    text(tft, 86, 89, macOnline ? "MAC ONLINE" : "MAC WAIT", 1,
         macOnline ? ui::theme().good : ui::theme().warning);
    homeCache.wifiOnline = wifiOnline;
    homeCache.macOnline = macOnline;
  }
  if (!homeCache.initialized || homeCache.hasMusic != hasMusic || strcmp(homeCache.music, music) != 0) {
    clearHomeRegion(tft, 0, 109, 160, 19);
    text(tft, 7, 115, hasMusic ? "NOW" : "NO MUSIC", 1, ui::theme().muted);
    if (hasMusic) text(tft, 35, 115, music, 1, ui::theme().text);
    homeCache.hasMusic = hasMusic;
    snprintf(homeCache.music, sizeof(homeCache.music), "%s", music);
  }
  if (!homeCache.initialized || homeCache.battery != battery) {
    clearHomeRegion(tft, 135, 0, 25, 16);
    if (battery >= 0) {
      char batteryText[8];
      snprintf(batteryText, sizeof(batteryText), "%u%%", static_cast<unsigned>(battery));
      text(tft, 136, 4, batteryText, 1, ui::theme().text);
    } else {
      text(tft, 142, 4, "--", 1, ui::theme().muted);
    }
    homeCache.battery = battery;
  }
  homeCache.initialized = true;
}

void renderMirror(Display& display, const AppState& state) {
  auto& tft = display.canvas();
  ui::clear(tft);
  header(tft, "MIRROR", state);
  if (state.mirrorActive) {
    text(tft, 64, 58, "LIVE", 1, ui::theme().good);
    return;
  }
  text(tft, 53, 44, "NO SIGNAL", 1, ui::theme().muted);
  text(tft, 42, 63, "START ON MAC", 1, ui::theme().warning);
  tft.drawFastHLine(5, 108, 150, ui::theme().panelAlt);
  text(tft, 6, 115, state.network == NetworkState::Online ? "WIFI OK" : "NO WIFI", 1, ui::theme().muted);
  text(tft, 125, 115, "IDLE", 1, ui::theme().warning);
}

void renderMusic(Display& display, const AppState& state) {
  ui::clear(display.canvas());
  header(display.canvas(), "AUDIO", state);
  renderMusicDynamic(display, state);
}

void renderMusicDynamic(Display& display, const AppState& state) {
  auto& tft = display.canvas();
  const bool fresh = telemetryFresh(state);
  const bool hasMusic = fresh && state.track[0] != '\0';
  tft.fillRect(0, 16, 160, 92, ui::theme().background);
  if (hasMusic) {
    text(tft, 8, 27, state.musicPlaying ? ">" : "||", 2, ui::theme().accent);
    text(tft, 30, 28, musicLabel(state.track, "NOW PLAYING"), 1, ui::theme().text);
    text(tft, 30, 46, musicLabel(state.artist, "UNKNOWN ARTIST"), 1, ui::theme().muted);
    text(tft, 30, 64, state.musicPlaying ? "PLAYING" : "PAUSED", 1,
         state.musicPlaying ? ui::theme().good : ui::theme().warning);
    const uint8_t progress = state.musicDuration == 0
                                 ? 0
                                 : static_cast<uint8_t>(min<uint32_t>(100, (static_cast<uint32_t>(state.musicPosition) * 100) / state.musicDuration));
    progressBar(tft, 8, 84, 144, progress, ui::theme().accent);
    char current[8];
    char total[8];
    snprintf(current, sizeof(current), "%02u:%02u", state.musicPosition / 60, state.musicPosition % 60);
    snprintf(total, sizeof(total), "%02u:%02u", state.musicDuration / 60, state.musicDuration % 60);
    text(tft, 8, 94, current, 1, ui::theme().muted);
    text(tft, 130, 94, total, 1, ui::theme().muted);
  } else {
    text(tft, 52, 43, "NO MUSIC", 1, ui::theme().muted);
    text(tft, 42, 62, "WAIT FOR MAC", 1, ui::theme().warning);
  }
  tft.drawFastHLine(5, 108, 150, ui::theme().panelAlt);
  text(tft, 6, 115, hasMusic ? "MAC AUDIO" : "MAC LINK", 1, ui::theme().muted);
  text(tft, 124, 115, fresh ? "LIVE" : "WAIT", 1, fresh ? ui::theme().good : ui::theme().warning);
}

void renderDashboard(Display& display, const AppState& state) {
  ui::clear(display.canvas());
  header(display.canvas(), "SYSTEM", state);
  renderDashboardDynamic(display, state);
}

void renderDashboardDynamic(Display& display, const AppState& state) {
  auto& tft = display.canvas();
  const bool fresh = telemetryFresh(state);
  const int cpu = fresh ? state.cpuPercent : -1;
  const int memory = fresh ? state.memoryPercent : -1;
  tft.fillRect(0, 16, 160, 92, ui::theme().background);
  metric(tft, 8, 25, "CPU", cpu, ui::theme().accent);
  progressBar(tft, 8, 39, 144, cpu < 0 ? 0 : static_cast<uint8_t>(cpu), ui::theme().accent);
  metric(tft, 8, 52, "MEM", memory, ui::theme().accent);
  progressBar(tft, 8, 66, 144, memory < 0 ? 0 : static_cast<uint8_t>(memory), ui::theme().accent);
  text(tft, 8, 82, "WIFI", 1, ui::theme().muted);
  text(tft, 43, 82, state.network == NetworkState::Online ? "ONLINE" : "WAIT", 1,
       state.network == NetworkState::Online ? ui::theme().good : ui::theme().warning);
  text(tft, 88, 82, "MAC", 1, ui::theme().muted);
  text(tft, 116, 82, fresh ? "SYNC" : "WAIT", 1, fresh ? ui::theme().good : ui::theme().warning);
  tft.drawFastHLine(5, 108, 150, ui::theme().panelAlt);
  text(tft, 6, 115, fresh ? "TELEMETRY" : "NO TELEMETRY", 1, ui::theme().muted);
  text(tft, 124, 115, fresh ? "LIVE" : "WAIT", 1, fresh ? ui::theme().good : ui::theme().warning);
}

void renderSettings(Display& display, const AppState& state) {
  auto& tft = display.canvas();
  ui::clear(tft);
  header(tft, "INFO", state);
  text(tft, 8, 26, "FIRMWARE", 1, ui::theme().muted);
  text(tft, 78, 26, "0.3.0", 1, ui::theme().text);
  text(tft, 8, 44, "DISPLAY", 1, ui::theme().muted);
  text(tft, 78, 44, "160 X 128", 1, ui::theme().text);
  text(tft, 8, 62, "NETWORK", 1, ui::theme().muted);
  text(tft, 78, 62, networkName(state.network), 1, ui::theme().text);
  text(tft, 8, 80, "CONTROL", 1, ui::theme().muted);
  text(tft, 78, 80, telemetryFresh(state) ? "CONNECTED" : "WAIT", 1, ui::theme().text);
  tft.drawFastHLine(5, 108, 150, ui::theme().panelAlt);
  text(tft, 6, 115, "DEVICE INFO", 1, ui::theme().muted);
}

void renderError(Display& display, const AppState& state) {
  auto& tft = display.canvas();
  ui::clear(tft);
  header(tft, "NOTICE", state);
  text(tft, 23, 43, "CONTROL LINK LOST", 1, ui::theme().text);
  text(tft, 47, 62, "CHECK WIFI", 1, ui::theme().danger);
  tft.drawFastHLine(5, 108, 150, ui::theme().panelAlt);
  text(tft, 6, 115, "RETRYING", 1, ui::theme().muted);
}

}  // namespace mini_tv::screens
