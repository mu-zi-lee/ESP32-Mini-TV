#pragma once

#include <WebServer.h>
#include <WiFi.h>

#include "../core/ModeManager.h"

namespace mini_tv {

class MirrorReceiver;

class NetworkManager {
 public:
  explicit NetworkManager(ModeManager& modes) : modes_(modes), server_(80) {}
  void setMirrorReceiver(MirrorReceiver& mirror) { mirror_ = &mirror; }
  void begin();
  void handle();
  const char* modeName() const { return mini_tv::modeName(modes_.state().mode); }

 private:
  void setupRoutes();
  void sendJson(const String& body, int code = 200);
  String jsonEscape(const String& value) const;
  bool jsonValue(const String& body, const char* key, String& value) const;
  void handleStatus();
  void handleMode();
  void handleWifi();
  void handleScan();
  void handleTelemetry();
  void handleMirror();
  void beginStationConnection();
  void loadWifiCredentials();

  ModeManager& modes_;
  WebServer server_;
  bool stationConnected_ = false;
  bool stationConnecting_ = false;
  String apName_ = "MiniTV-Setup";
  String stationSsid_;
  String stationPassword_;
  uint32_t lastConnectAttempt_ = 0;
  MirrorReceiver* mirror_ = nullptr;
};

}  // namespace mini_tv
