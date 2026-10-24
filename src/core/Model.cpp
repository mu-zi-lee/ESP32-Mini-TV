#include "Model.h"

namespace mini_tv {

const char* modeName(Mode mode) {
  switch (mode) {
    case Mode::Home: return "HOME";
    case Mode::Mirror: return "MIRROR";
    case Mode::Music: return "MUSIC";
    case Mode::Dashboard: return "DASH";
    case Mode::Settings: return "SETTINGS";
    case Mode::Error: return "ERROR";
  }
  return "UNKNOWN";
}

const char* networkName(NetworkState state) {
  switch (state) {
    case NetworkState::Offline: return "OFFLINE";
    case NetworkState::Connecting: return "CONNECTING";
    case NetworkState::Online: return "ONLINE";
  }
  return "UNKNOWN";
}

}  // namespace mini_tv
