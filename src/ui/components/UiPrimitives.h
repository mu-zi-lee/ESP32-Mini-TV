#pragma once

#include <Adafruit_GFX.h>
#include "../../core/Model.h"

namespace mini_tv::ui {

struct Theme {
  uint16_t background, panel, panelAlt, text, muted, accent, good, warning, danger;
};

const Theme& theme();
void clear(Adafruit_GFX& display);
void header(Adafruit_GFX& display, const char* title, const AppState& state);
void card(Adafruit_GFX& display, int16_t x, int16_t y, int16_t w, int16_t h, uint16_t color);
void statusPill(Adafruit_GFX& display, int16_t x, int16_t y, const char* label, uint16_t color);
void progressBar(Adafruit_GFX& display, int16_t x, int16_t y, int16_t w, uint8_t percent, uint16_t color);
void text(Adafruit_GFX& display, int16_t x, int16_t y, const char* value, uint8_t size, uint16_t color);

}  // namespace mini_tv::ui
