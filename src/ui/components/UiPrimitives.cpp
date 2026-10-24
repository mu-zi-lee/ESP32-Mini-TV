#include "UiPrimitives.h"

#include <string.h>


namespace mini_tv::ui {

const Theme& theme() {
  static const Theme kTheme{0x0841, 0x18E3, 0x10A2, 0xFFFF, 0x9CF3, 0x05FF, 0x07E0, 0xFD20, 0xF800};
  return kTheme;
}

void clear(Adafruit_GFX& display) { display.fillScreen(theme().background); }

void text(Adafruit_GFX& display, int16_t x, int16_t y, const char* value, uint8_t size, uint16_t color) {
  display.setTextSize(size);
  display.setTextColor(color);
  display.setCursor(x, y);
  display.print(value);
}

void header(Adafruit_GFX& display, const char* title, const AppState& state) {
  display.fillRect(0, 0, 160, 16, theme().panelAlt);
  text(display, 5, 4, "MINI TV", 1, theme().text);
  text(display, 66, 4, title, 1, theme().accent);
  const uint16_t connection = state.network == NetworkState::Online
                                  ? theme().good
                                  : (state.network == NetworkState::Connecting ? theme().warning : theme().danger);
  display.fillCircle(132, 8, 3, connection);
  if (state.telemetryValid) {
    char battery[8];
    snprintf(battery, sizeof(battery), "%u%%", state.batteryPercent);
    text(display, 136, 4, battery, 1, theme().text);
  } else {
    text(display, 142, 4, "--", 1, theme().muted);
  }
}

void card(Adafruit_GFX& display, int16_t x, int16_t y, int16_t w, int16_t h, uint16_t color) {
  display.fillRoundRect(x, y, w, h, 4, color);
  display.drawRoundRect(x, y, w, h, 4, theme().muted);
}

void statusPill(Adafruit_GFX& display, int16_t x, int16_t y, const char* label, uint16_t color) {
  int16_t width = 10;
  for (const uint8_t* cursor = reinterpret_cast<const uint8_t*>(label); *cursor;) {
    if (*cursor < 0x80) { width += 6; cursor += 1; }
    else if ((*cursor & 0xE0) == 0xC0) { width += 16; cursor += 2; }
    else { width += 16; cursor += 3; }
  }
  display.fillRoundRect(x, y, width, 14, 3, color);
  text(display, x + 5, y + 4, label, 1, theme().background);
}

void progressBar(Adafruit_GFX& display, int16_t x, int16_t y, int16_t w, uint8_t percent, uint16_t color) {
  display.drawRoundRect(x, y, w, 7, 2, theme().muted);
  const int16_t fill = static_cast<int16_t>((w - 2) * percent / 100);
  if (fill > 0) display.fillRect(x + 1, y + 1, fill, 5, color);
}

}  // namespace mini_tv::ui
