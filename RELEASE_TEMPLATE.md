## ESP32 Mini TV release

### Control downloads

- macOS: open the `.dmg`, drag **Control** to Applications, then grant Screen
  Recording permission when using screen mirroring.
- Windows: run `control.exe` (or the `Control Setup *.exe` installer when the
  release includes one). Ordinary screen mirroring is supported; the macOS
  virtual-display feature is not available.
- Linux: extract the `linux-*.tar.gz` archive and run `install.sh`, or install
  the `.deb` package. Ordinary screen mirroring depends on the desktop capture
  tools available on the distribution.
- Browser: open the `web` package from a static web server. Device control
  still requires the browser to reach the ESP32 on the local network.

### ESP32 firmware

Build and upload the firmware with PlatformIO from the repository root:

```sh
pio run
pio run -t upload --upload-port /dev/cu.usbserial-XXXX
```

The firmware keeps the `esp32dev` target and the ST7735 wiring documented in
the README. PlatformIO is intentionally not bundled in the desktop installers.

### Agent Status hook

Install the skill from this repository by copying
`companion/control/skills/agent-status` into your Codex skills directory, or
use the checked-in hook directly:

```sh
mkdir -p ~/.codex/skills/mini-tv-agent-status
cp companion/control/skills/agent-status/SKILL.md ~/.codex/skills/mini-tv-agent-status/
```

Configure the hook command in `~/.codex/hooks.json`, trust it in `/hooks`, and
restart the Codex session. The traffic-light meaning is **green = working**,
**red = stopped/finished**, and **yellow = attention needed**.
