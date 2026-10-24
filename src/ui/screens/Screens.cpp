#include "Screens.h"

#include "../components/UiPrimitives.h"

namespace mini_tv::screens {
using ui::card;
using ui::header;
using ui::progressBar;
using ui::statusPill;
using ui::text;

namespace {
const char* musicLabel(const char* value, const char* fallback) {
  // The 1.8-inch panel uses a compact ASCII font. Keep readable ASCII from
  // browser/player titles while dropping emoji or other unsupported glyphs;
  // this is more useful than replacing an otherwise valid title wholesale.
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
}  // namespace

void renderHome(Display& display, const AppState& state) {
  auto& tft = display.canvas();
  ui::clear(tft);
  header(tft, "MINI TV", state);
  tft.drawFastHLine(6, 21, 148, ui::theme().accent);
  text(tft, 8, 29, "12:00", 3, ui::theme().text);
  text(tft, 92, 31, "THU", 1, ui::theme().muted);
  text(tft, 92, 47, "OCT 23", 1, ui::theme().muted);
  card(tft, 7, 68, 68, 35, ui::theme().panel);
  text(tft, 14, 74, "WEATHER", 1, ui::theme().muted);
  text(tft, 14, 90, "-- C", 2, ui::theme().accent);
  card(tft, 83, 68, 68, 35, ui::theme().panel);
  text(tft, 90, 74, "DEVICE", 1, ui::theme().muted);
  renderHomeDynamic(display, state);
  statusPill(tft, 7, 111, state.network == NetworkState::Online ? "ONLINE" : "OFFLINE",
             state.network == NetworkState::Online ? ui::theme().good : ui::theme().warning);
  text(tft, 82, 115, "REMOTE", 1, ui::theme().muted);
}

void renderHomeDynamic(Display& display, const AppState& state) {
  auto& tft = display.canvas();
  tft.fillRect(90, 88, 54, 14, ui::theme().panel);
  char uptime[12];
  snprintf(uptime, sizeof(uptime), "%lus", (millis() - state.startedAt) / 1000UL);
  text(tft, 90, 90, uptime, 2, ui::theme().good);
}

void renderMirror(Display& display, const AppState& state) {
  auto& tft = display.canvas();
  ui::clear(tft);
  header(tft, "MIRROR", state);
  card(tft, 7, 25, 144, 55, ui::theme().panel);
  tft.drawRect(15, 33, 128, 34, ui::theme().muted);
  tft.fillRect(19, 37, 120, 26, ui::theme().panelAlt);
  text(tft, 52, 44, state.mirrorActive ? "LIVE" : "NO SIGNAL", 1,
       state.mirrorActive ? ui::theme().good : ui::theme().muted);
  text(tft, 9, 87, state.mirrorActive ? "MAC STREAM" : "START ON MAC", 1, ui::theme().text);
  text(tft, 9, 103, state.network == NetworkState::Online ? "WIFI OK" : "NO WIFI", 1, ui::theme().accent);
  progressBar(tft, 9, 119, 104, state.mirrorActive ? 100 : 0,
              state.mirrorActive ? ui::theme().good : ui::theme().accent);
  statusPill(tft, 119, 116, state.mirrorActive ? "LIVE" : "IDLE",
             state.mirrorActive ? ui::theme().good : ui::theme().muted);
}

void renderMusic(Display& display, const AppState& state) {
  auto& tft = display.canvas();
  ui::clear(tft);
  header(tft, "AUDIO", state);
  card(tft, 8, 27, 52, 52, ui::theme().panel);
  tft.fillRect(18, 37, 32, 32, ui::theme().accent);
  text(tft, 20, 49, "PLAY", 1, ui::theme().background);
  renderMusicDynamic(display, state);
}

void renderMusicDynamic(Display& display, const AppState& state) {
  auto& tft = display.canvas();
  const bool hasMusic = state.telemetryValid && state.track[0] != '\0';
  tft.fillRect(68, 27, 84, 58, ui::theme().background);
  text(tft, 70, 31, hasMusic ? musicLabel(state.track, "NOW PLAYING") : "NOT PLAYING", 1,
       hasMusic ? ui::theme().text : ui::theme().warning);
  text(tft, 70, 47, hasMusic ? musicLabel(state.artist, "MAC MUSIC") : "WAITING FOR MAC", 1,
       ui::theme().text);
  text(tft, 70, 66, hasMusic ? (state.musicPlaying ? "PLAYING" : "PAUSED") : "WAITING", 1,
       hasMusic ? ui::theme().accent : ui::theme().muted);
  const uint8_t progress = state.musicDuration == 0 ? 0 : static_cast<uint8_t>(min<uint32_t>(100, (static_cast<uint32_t>(state.musicPosition) * 100) / state.musicDuration));
  tft.fillRect(8, 90, 144, 26, ui::theme().background);
  progressBar(tft, 9, 91, 142, progress, ui::theme().accent);
  char current[8]; char total[8];
  snprintf(current, sizeof(current), "%02u:%02u", state.musicPosition / 60, state.musicPosition % 60);
  snprintf(total, sizeof(total), "%02u:%02u", state.musicDuration / 60, state.musicDuration % 60);
  text(tft, 9, 103, hasMusic ? current : "--:--", 1, ui::theme().muted);
  text(tft, 132, 103, hasMusic ? total : "--:--", 1, ui::theme().muted);
  statusPill(tft, 9, 113, hasMusic ? (state.musicPlaying ? "PLAY" : "PAUSE") : "WAIT", hasMusic ? ui::theme().good : ui::theme().warning);
}

void renderDashboard(Display& display, const AppState& state) {
  auto& tft = display.canvas();
  ui::clear(tft);
  header(tft, "SYSTEM", state);
  card(tft, 7, 25, 68, 47, ui::theme().panel);
  text(tft, 13, 31, "CPU", 1, ui::theme().muted);
  card(tft, 83, 25, 68, 47, ui::theme().panel);
  text(tft, 89, 31, "MEM", 1, ui::theme().muted);
  card(tft, 7, 79, 144, 30, ui::theme().panelAlt);
  text(tft, 13, 85, "MAC LINK", 1, ui::theme().muted);
  renderDashboardDynamic(display, state);
}

void renderDashboardDynamic(Display& display, const AppState& state) {
  auto& tft = display.canvas();
  tft.fillRect(12, 47, 58, 20, ui::theme().panel);
  char cpu[8]; snprintf(cpu, sizeof(cpu), "%u%%", state.telemetryValid ? state.cpuPercent : 0);
  text(tft, 13, 49, state.telemetryValid ? cpu : "--", 2, ui::theme().accent);
  tft.fillRect(12, 64, 58, 7, ui::theme().panel);
  progressBar(tft, 13, 65, 54, state.telemetryValid ? state.cpuPercent : 0, ui::theme().accent);
  tft.fillRect(88, 47, 58, 20, ui::theme().panel);
  char memory[8]; snprintf(memory, sizeof(memory), "%u%%", state.telemetryValid ? state.memoryPercent : 0);
  text(tft, 89, 49, state.telemetryValid ? memory : "--", 2, ui::theme().accent);
  tft.fillRect(88, 64, 58, 7, ui::theme().panel);
  progressBar(tft, 89, 65, 54, state.telemetryValid ? state.memoryPercent : 0, ui::theme().accent);
  tft.fillRect(86, 83, 64, 18, ui::theme().panelAlt);
  text(tft, 89, 85, state.telemetryValid ? "SYNC" : "WAIT", 1, state.telemetryValid ? ui::theme().good : ui::theme().warning);
  tft.fillRect(5, 111, 150, 17, ui::theme().background);
  statusPill(tft, 7, 113, state.telemetryValid ? "SYNC" : "WAIT", state.telemetryValid ? ui::theme().good : ui::theme().warning);
}

void renderSettings(Display& display, const AppState& state) {
  auto& tft = display.canvas();
  ui::clear(tft);
  header(tft, "SETTINGS", state);
  card(tft, 7, 25, 144, 80, ui::theme().panel);
  text(tft, 14, 31, "ROTATION", 1, ui::theme().muted); text(tft, 100, 31, "LAND", 1, ui::theme().accent);
  text(tft, 14, 49, "NETWORK", 1, ui::theme().muted); text(tft, 100, 49, state.network == NetworkState::Online ? "ONLINE" : "OFFLINE", 1, state.network == NetworkState::Online ? ui::theme().good : ui::theme().warning);
  text(tft, 14, 67, "FIRMWARE", 1, ui::theme().muted); text(tft, 100, 69, "0.3.0", 1, ui::theme().text);
  text(tft, 14, 85, "CONTROL", 1, ui::theme().muted); text(tft, 100, 85, "REMOTE", 1, ui::theme().accent);
  statusPill(tft, 7, 113, "READY", ui::theme().good);
}

void renderError(Display& display, const AppState& state) {
  auto& tft = display.canvas();
  ui::clear(tft);
  header(tft, "NOTICE", state);
  card(tft, 9, 31, 142, 56, ui::theme().panel);
  text(tft, 20, 44, "CHECK DEVICE", 1, ui::theme().text);
  text(tft, 20, 64, "USE MAC APP", 1, ui::theme().danger);
  statusPill(tft, 9, 96, "OK", ui::theme().good);
}

}  // namespace mini_tv::screens
