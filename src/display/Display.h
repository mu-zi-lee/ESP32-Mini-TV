#pragma once

#include <Adafruit_ST7735.h>

namespace mini_tv {

class Display {
 public:
  // The panel is physically 128x160. Rotation 1 exposes it as 160x128.
  static constexpr int16_t Width = 160;
  static constexpr int16_t Height = 128;
  void begin();
  void runHardwareSmokeTest();
  Adafruit_ST7735& canvas() { return tft_; }

 private:
  Adafruit_ST7735 tft_{27, 26, 25};
};

}  // namespace mini_tv
