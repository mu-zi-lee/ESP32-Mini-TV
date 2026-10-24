#include "NetworkManager.h"

#include <ESPmDNS.h>
#include <Preferences.h>

#include "MirrorReceiver.h"

#if __has_include("wifi_config.h")
#include "wifi_config.h"
#endif

#ifndef MINITV_WIFI_SSID
#define MINITV_WIFI_SSID ""
#endif

#ifndef MINITV_WIFI_PASSWORD
#define MINITV_WIFI_PASSWORD ""
#endif

namespace mini_tv {
namespace {
Preferences preferences;

String chipSuffix() {
  const uint32_t id = static_cast<uint32_t>(ESP.getEfuseMac());
  char suffix[5];
  snprintf(suffix, sizeof(suffix), "%04X", static_cast<unsigned>(id & 0xFFFF));
  return String(suffix);
}
}  // namespace

void NetworkManager::begin() {
  WiFi.mode(WIFI_AP_STA);
  WiFi.setAutoReconnect(true);
  WiFi.persistent(false);
  apName_ += "-" + chipSuffix();
  WiFi.softAP(apName_.c_str(), "minitv-setup");
  loadWifiCredentials();
  beginStationConnection();
  setupRoutes();
  server_.begin();
  Serial.printf("Control AP: %s @ %s\n", apName_.c_str(), WiFi.softAPIP().toString().c_str());
  if (stationSsid_.isEmpty()) {
    Serial.println("WiFi station: not configured (use .env or the local setup page)");
  } else {
    Serial.printf("WiFi station: connecting to %s\n", stationSsid_.c_str());
  }
}

void NetworkManager::handle() {
  server_.handleClient();
  const bool connected = WiFi.status() == WL_CONNECTED;
  if (connected != stationConnected_) {
    stationConnected_ = connected;
    stationConnecting_ = false;
    modes_.setNetworkState(connected ? NetworkState::Online : NetworkState::Offline);
    modes_.render();
    if (connected) {
      MDNS.begin("mini-tv");
      Serial.printf("WiFi connected: %s (%d dBm)\n", WiFi.localIP().toString().c_str(), WiFi.RSSI());
    } else {
      Serial.println("WiFi disconnected; retrying automatically");
    }
  }

  // Keep the setup AP available while retrying the saved station network.
  // This makes a device recoverable even when the home router is temporarily offline.
  if (!stationSsid_.isEmpty() && !connected && millis() - lastConnectAttempt_ >= 15000) {
    beginStationConnection();
  }
}

void NetworkManager::setupRoutes() {
  server_.on("/api/status", HTTP_GET, [this]() { handleStatus(); });
  server_.on("/api/mode", HTTP_POST, [this]() { handleMode(); });
  server_.on("/api/mode", HTTP_OPTIONS, [this]() { sendJson("{}", 204); });
  server_.on("/api/wifi", HTTP_POST, [this]() { handleWifi(); });
  server_.on("/api/wifi", HTTP_OPTIONS, [this]() { sendJson("{}", 204); });
  server_.on("/api/scan", HTTP_GET, [this]() { handleScan(); });
  server_.on("/api/telemetry", HTTP_POST, [this]() { handleTelemetry(); });
  server_.on("/api/telemetry", HTTP_OPTIONS, [this]() { sendJson("{}", 204); });
  server_.on("/api/mirror", HTTP_POST, [this]() { handleMirror(); });
  server_.on("/api/mirror", HTTP_OPTIONS, [this]() { sendJson("{}", 204); });
  server_.onNotFound([this]() { sendJson("{\"ok\":false,\"error\":\"not_found\"}", 404); });
}

void NetworkManager::sendJson(const String& body, int code) {
  server_.sendHeader("Access-Control-Allow-Origin", "*");
  server_.sendHeader("Access-Control-Allow-Methods", "GET,POST,OPTIONS");
  server_.sendHeader("Access-Control-Allow-Headers", "Content-Type");
  server_.send(code, "application/json", body);
}

String NetworkManager::jsonEscape(const String& value) const {
  String result;
  result.reserve(value.length() + 4);
  for (size_t i = 0; i < value.length(); ++i) {
    const char c = value[i];
    if (c == '"' || c == '\\') result += '\\';
    result += c;
  }
  return result;
}

bool NetworkManager::jsonValue(const String& body, const char* key, String& value) const {
  const String needle = String("\"") + key + "\"";
  int start = body.indexOf(needle);
  if (start < 0) return false;
  start = body.indexOf(':', start + needle.length());
  if (start < 0) return false;
  start++;
  while (start < static_cast<int>(body.length()) && body[start] == ' ') start++;
  const bool quoted = start < static_cast<int>(body.length()) && body[start] == '"';
  if (quoted) start++;
  int end = start;
  while (end < static_cast<int>(body.length()) && body[end] != '"' && body[end] != ',' && body[end] != '}') end++;
  value = body.substring(start, end);
  value.trim();
  return value.length() > 0 || quoted;
}

void NetworkManager::loadWifiCredentials() {
  preferences.begin("wifi", true);
  const String ssid = preferences.getString("ssid", "");
  const String wifiKey = preferences.getString("pass", "");
  preferences.end();
  stationSsid_ = ssid.isEmpty() ? String(MINITV_WIFI_SSID) : ssid;
  stationPassword_ = ssid.isEmpty() ? String(MINITV_WIFI_PASSWORD) : wifiKey;
}

void NetworkManager::beginStationConnection() {
  if (stationSsid_.isEmpty()) {
    stationConnecting_ = false;
    return;
  }
  lastConnectAttempt_ = millis();
  stationConnecting_ = true;
  modes_.setNetworkState(NetworkState::Connecting);
  WiFi.disconnect(false, false);
  WiFi.begin(stationSsid_.c_str(), stationPassword_.c_str());
  Serial.printf("WiFi: attempting %s\n", stationSsid_.c_str());
}

void NetworkManager::handleStatus() {
  const String deviceIp = stationConnected_ ? WiFi.localIP().toString() : WiFi.softAPIP().toString();
  const String body = String("{\"ok\":true,\"mode\":\"") + modeName() +
                      "\",\"stationConnected\":" + (stationConnected_ ? "true" : "false") +
                      ",\"stationIp\":\"" + jsonEscape(WiFi.localIP().toString()) +
                      "\",\"setupSsid\":\"" + jsonEscape(apName_) +
                      "\",\"setupIp\":\"" + WiFi.softAPIP().toString() +
                      "\",\"deviceIp\":\"" + deviceIp +
                      "\",\"rssi\":" + String(stationConnected_ ? WiFi.RSSI() : 0) +
                      ",\"telemetryValid\":" + String(modes_.state().telemetryValid ? "true" : "false") +
                      ",\"playing\":" + String(modes_.state().musicPlaying ? "true" : "false") +
                      ",\"track\":\"" + jsonEscape(String(modes_.state().track)) +
                      "\",\"artist\":\"" + jsonEscape(String(modes_.state().artist)) + "\"" +
                      ",\"mirrorActive\":" + String(modes_.state().mirrorActive ? "true" : "false") +
                      ",\"mirrorFrames\":" + String(modes_.state().mirrorFrames) +
                      ",\"mirrorUdpBound\":" + String(mirror_ && mirror_->udpBound() ? "true" : "false") +
                      ",\"mirrorPackets\":" + String(mirror_ ? mirror_->packetsSeen() : 0) +
                      ",\"mirrorRejected\":" + String(mirror_ ? mirror_->packetsRejected() : 0) +
                      ",\"mirrorDecodeFailures\":" + String(mirror_ ? mirror_->framesFailed() : 0) + "}";
  sendJson(body);
}

void NetworkManager::handleTelemetry() {
  const String body = server_.arg("plain");
  if (body.length() > 512) {
    sendJson("{\"ok\":false,\"error\":\"body_too_large\"}", 413);
    return;
  }
  String cpu, memory, battery, charging, playing, track, artist, position, duration;
  const bool valid = jsonValue(body, "cpu", cpu) && jsonValue(body, "memory", memory) &&
                     jsonValue(body, "battery", battery) && jsonValue(body, "charging", charging) &&
                     jsonValue(body, "playing", playing) && jsonValue(body, "track", track) &&
                     jsonValue(body, "artist", artist) && jsonValue(body, "position", position) &&
                     jsonValue(body, "duration", duration);
  if (!valid || track.length() >= 48 || artist.length() >= 40) {
    sendJson("{\"ok\":false,\"error\":\"invalid_telemetry\"}", 400);
    return;
  }
  modes_.updateTelemetry(static_cast<uint8_t>(constrain(cpu.toInt(), 0, 100)),
                         static_cast<uint8_t>(constrain(memory.toInt(), 0, 100)),
                         static_cast<uint8_t>(constrain(battery.toInt(), 0, 100)),
                         charging == "true" || charging == "1", playing == "true" || playing == "1",
                         track.c_str(), artist.c_str(),
                         static_cast<uint16_t>(constrain(position.toInt(), 0, 65535)),
                         static_cast<uint16_t>(constrain(duration.toInt(), 0, 65535)));
  sendJson("{\"ok\":true}");
}

void NetworkManager::handleMirror() {
  String active;
  if (!jsonValue(server_.arg("plain"), "active", active)) {
    sendJson("{\"ok\":false,\"error\":\"invalid_mirror\"}", 400);
    return;
  }
  modes_.setMirrorActive(active == "true" || active == "1");
  sendJson(String("{\"ok\":true,\"active\":") + (modes_.state().mirrorActive ? "true" : "false") + "}");
}

void NetworkManager::handleMode() {
  if (server_.arg("plain").length() > 512) {
    sendJson("{\"ok\":false,\"error\":\"body_too_large\"}", 413);
    return;
  }
  String mode;
  if (!jsonValue(server_.arg("plain"), "mode", mode) || !modes_.setModeByName(mode.c_str())) {
    sendJson("{\"ok\":false,\"error\":\"invalid_mode\"}", 400);
    return;
  }
  sendJson(String("{\"ok\":true,\"mode\":\"") + modeName() + "\"}");
}

void NetworkManager::handleWifi() {
  const String body = server_.arg("plain");
  if (body.length() > 256) {
    sendJson("{\"ok\":false,\"error\":\"body_too_large\"}", 413);
    return;
  }
  String ssid;
  String wifiKey;
  const bool hasSsid = jsonValue(body, "ssid", ssid);
  const bool hasPassword = jsonValue(body, "pass", wifiKey) || jsonValue(body, "password", wifiKey) ||
                           body.indexOf("\"pass\"") >= 0 || body.indexOf("\"password\"") >= 0;
  if (!hasSsid || ssid.isEmpty() || !hasPassword || ssid.length() > 32 || wifiKey.length() > 63) {
    sendJson("{\"ok\":false,\"error\":\"invalid_wifi\"}", 400);
    return;
  }
  preferences.begin("wifi", false);
  preferences.putString("ssid", ssid);
  preferences.putString("pass", wifiKey);
  preferences.end();
  stationSsid_ = ssid;
  stationPassword_ = wifiKey;
  beginStationConnection();
  sendJson("{\"ok\":true,\"message\":\"saved_and_connecting\"}");
}

void NetworkManager::handleScan() {
  const int count = WiFi.scanNetworks(false, true);
  String body = "{\"ok\":true,\"networks\":[";
  for (int i = 0; i < count && i < 12; ++i) {
    if (i) body += ',';
    body += "{\"ssid\":\"" + jsonEscape(WiFi.SSID(i)) + "\",\"rssi\":" + String(WiFi.RSSI(i)) +
            ",\"secure\":" + String(WiFi.encryptionType(i) != WIFI_AUTH_OPEN ? "true" : "false") + "}";
  }
  body += "]}";
  WiFi.scanDelete();
  sendJson(body);
}

}  // namespace mini_tv
