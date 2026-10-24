#include "Display.h"

#include <Arduino.h>
#include <SPI.h>

namespace mini_tv {

void Display::begin() {
  // These pins and BLACKTAB are part of the verified hardware contract.
  SPI.begin(18, -1, 23, 27);
  tft_.initR(INITR_BLACKTAB);
  // Verified on the current ST7735R wiring; this reduces full-frame flush
  // time without changing the 160x128 landscape canvas.
  tft_.setSPISpeed(26000000UL);
  tft_.setRotation(1);
  tft_.setTextWrap(false);
}

void Display::runHardwareSmokeTest() {
  tft_.fillScreen(ST77XX_RED);
  delay(180);
  tft_.fillScreen(ST77XX_GREEN);
  delay(180);
  tft_.fillScreen(ST77XX_BLUE);
  delay(180);
  tft_.fillScreen(ST77XX_BLACK);
}

}  // namespace mini_tv
