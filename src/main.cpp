#include <Arduino.h>
#include <SPI.h>
#include <Adafruit_GFX.h>
#include <Adafruit_ST7735.h>

#define TFT_CS 27
#define TFT_DC 26
#define TFT_RST 25
#define TFT_SCLK 18
#define TFT_MOSI 23

Adafruit_ST7735 tft(TFT_CS, TFT_DC, TFT_RST);

void setup() {
  Serial.begin(115200);
  delay(500);

  Serial.println("Starting Mini TV...");

  SPI.begin(TFT_SCLK, -1, TFT_MOSI, TFT_CS);
  tft.initR(INITR_BLACKTAB);
  tft.setRotation(0);

  tft.fillScreen(ST77XX_RED);
  delay(700);
  tft.fillScreen(ST77XX_GREEN);
  delay(700);
  tft.fillScreen(ST77XX_BLUE);
  delay(700);

  tft.fillScreen(ST77XX_BLACK);
  tft.fillRoundRect(5, 5, 118, 150, 8, tft.color565(20, 32, 50));

  tft.setTextColor(ST77XX_CYAN);
  tft.setTextSize(2);
  tft.setCursor(12, 20);
  tft.print("MINI TV");

  tft.drawFastHLine(12, 48, 104, ST77XX_WHITE);

  tft.setTextSize(1);
  tft.setTextColor(ST77XX_WHITE);
  tft.setCursor(12, 65);
  tft.print("ESP32 + ST7735");
  tft.setCursor(12, 85);
  tft.print("Display: OK");
  tft.setCursor(12, 100);
  tft.print("WiFi: Pending");

  tft.fillRoundRect(12, 125, 104, 20, 4, ST77XX_GREEN);
  tft.setTextColor(ST77XX_BLACK);
  tft.setCursor(43, 131);
  tft.print("READY");

  Serial.println("Display test complete!");
}

void loop() {
}
