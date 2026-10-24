#include <Arduino.h>

#include "core/ModeManager.h"
#include "network/MirrorReceiver.h"
#include "network/NetworkManager.h"
#include "ui/screens/Screens.h"

namespace {
mini_tv::Display display;
mini_tv::ModeManager modes(display);
mini_tv::NetworkManager network(modes);
mini_tv::MirrorReceiver mirror(modes, display);
String command;
}

void setup() {
  Serial.begin(115200);
  delay(500);
  Serial.println("Starting Mini TV Phase 2...");
  display.begin();
  display.runHardwareSmokeTest();
  modes.begin();
  network.setMirrorReceiver(mirror);
  network.begin();
  mirror.begin();
  Serial.println("Ready. Commands: home, mirror, music, dashboard, settings, error, next");
}

void loop() {
  network.handle();
  mirror.handle();
  while (Serial.available()) {
    const char incoming = static_cast<char>(Serial.read());
    if (incoming == '\n' || incoming == '\r') {
      command.trim();
      if (command == "next") {
        modes.next();
        Serial.println("Mode changed.");
      } else if (modes.setModeByName(command.c_str())) {
        Serial.print("Mode: ");
        Serial.println(mini_tv::modeName(modes.state().mode));
      } else if (command.length() > 0) {
        Serial.println("Unknown command.");
      }
      command = "";
    } else if (isPrintable(incoming) && command.length() < 24) {
      command += incoming;
    }
  }

  static uint32_t lastRefresh = 0;
  if (millis() - lastRefresh > 1000) {
    lastRefresh = millis();
    if (modes.state().mode == mini_tv::Mode::Home) mini_tv::screens::renderHomeDynamic(display, modes.state());
  }
}
