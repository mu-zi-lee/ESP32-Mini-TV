import { DeviceService, type DeviceStatus, type Network } from "./mygo";

const $ = <T extends HTMLElement>(selector: string) => document.querySelector<T>(selector)!;
const address = $<HTMLInputElement>("#address");
const statusText = $("#status");
const statusGrid = $("#status-grid");
const networkSelect = $<HTMLSelectElement>("#network");
const ssidInput = $<HTMLInputElement>("#ssid");
const passwordInput = $<HTMLInputElement>("#password");
const connectionLabel = $("#connection-label");
const modeLabel = $("#device-mode");
const syncButton = $("#sync-computer") as HTMLButtonElement;
const mirrorButton = $("#mirror-toggle") as HTMLButtonElement;
const autoSyncButton = $("#auto-sync") as HTMLButtonElement;
const clientAddress = $("#client-address");
const computerCpu = $("#computer-cpu");
const computerMemory = $("#computer-memory");
const computerBattery = $("#computer-battery");
const computerAudio = $("#computer-audio");
const computerArtist = $("#computer-artist");
const syncState = $("#sync-state");
const virtualTrack = $("#virtual-track");
const virtualStatus = $("#virtual-status");
const virtualClock = $("#virtual-clock");
const virtualOpen = $("#virtual-open") as HTMLButtonElement;
const virtualMirror = $("#virtual-mirror") as HTMLButtonElement;
const virtualDisplayTag = $("#virtual-display-tag");
const virtualResolution = $("#virtual-resolution");
const virtualDisplayID = $("#virtual-display-id");
const virtualHelp = $("#virtual-help");
let syncing = false;
let mirroring = false;
let virtualActive = false;
let virtualMirroring = false;
let autoSync = true;
let statusRefreshInFlight = false;

// Values come from a local .env file. They are only used as initial form
// values; no connection or Wi-Fi request is sent until the user clicks it.
const configuredAddress = (import.meta.env.MINI_TV_DEVICE_URL ?? import.meta.env.VITE_MINI_TV_DEVICE_URL ?? "").trim();
const savedAddress = localStorage.getItem("mini-tv.device-address")?.trim() ?? "";
address.value = savedAddress || configuredAddress || "http://192.168.4.1";
const configuredSSID = (import.meta.env.MINI_TV_WIFI_SSID ?? import.meta.env.VITE_MINI_TV_WIFI_SSID ?? "").trim();
const configuredPassword = import.meta.env.MINI_TV_WIFI_PASSWORD ?? import.meta.env.VITE_MINI_TV_WIFI_PASSWORD ?? "";
if (configuredSSID) ssidInput.value = configuredSSID;
if (configuredPassword) passwordInput.value = configuredPassword;

address.addEventListener("change", () => localStorage.setItem("mini-tv.device-address", address.value.trim()));

function showMessage(message: string, error = false) { statusText.textContent = message; statusText.className = error ? "notice error" : "notice"; }
function setConnection(connected: boolean) { connectionLabel.textContent = connected ? "已连接" : "未连接"; connectionLabel.parentElement?.classList.toggle("is-live", connected); }
function setSyncState(active: boolean) { syncState.textContent = active ? "LIVE" : "WAIT"; syncState.style.color = active ? "var(--green)" : ""; }
function renderComputer(payload: Awaited<ReturnType<typeof DeviceService.computerStatus>>) {
  computerCpu.textContent = payload.cpu > 0 ? String(payload.cpu) : "--";
  computerMemory.textContent = `${payload.memory}%`;
  computerBattery.textContent = `${payload.battery}%${payload.charging ? " · CHG" : ""}`;
  const hasAudio = payload.track.trim().length > 0;
  computerAudio.textContent = hasAudio ? payload.track : "未播放";
  computerArtist.textContent = hasAudio ? `${payload.artist || "未知艺术家"} · ${payload.playing ? "正在播放" : "已暂停"}` : "没有检测到正在运行的播放器";
  virtualTrack.textContent = hasAudio ? payload.track : "未播放";
  virtualStatus.textContent = hasAudio ? `${payload.artist || "未知艺术家"} · ${payload.playing ? "PLAYING" : "PAUSED"}` : "等待电脑音频";
  virtualClock.textContent = new Date().toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}
function renderVirtualDisplay(status: Awaited<ReturnType<typeof DeviceService.virtualDisplayStatus>>) {
  virtualActive = status.active;
  virtualDisplayTag.textContent = status.active ? "在线" : "未创建";
  virtualDisplayTag.classList.toggle("active", status.active);
  virtualDisplayID.textContent = status.active ? String(status.displayId) : "--";
  virtualResolution.textContent = `${status.width || 640} × ${status.height || 512} / 30 Hz`;
  virtualOpen.textContent = status.active ? "关闭虚拟屏幕" : "创建虚拟屏幕";
  virtualOpen.classList.toggle("button-secondary", status.active);
  virtualOpen.classList.toggle("button-primary", !status.active);
  virtualMirror.disabled = !status.active;
  virtualMirror.textContent = virtualMirroring ? "停止发送" : "发送到 ESP32";
  virtualMirror.classList.toggle("selected", virtualMirroring);
  if (status.active) {
    virtualHelp.textContent = "已创建系统显示器，可在系统设置中排列并拖动窗口";
  } else {
    virtualHelp.textContent = "创建后会出现在 macOS「显示器」设置中";
  }
}
async function refreshComputer() {
  try { renderComputer(await DeviceService.computerStatus()); } catch { computerArtist.textContent = "电脑状态读取失败"; }
}
function renderStatus(status: DeviceStatus) {
  setConnection(true); modeLabel.textContent = status.mode === "HOME" ? "主页" : status.mode; modeLabel.className = "tag active";
  mirroring = status.mirrorActive;
  mirrorButton.textContent = mirroring ? "停止镜像" : "启动镜像";
  mirrorButton.classList.toggle("selected", mirroring);
  const stationIP = status.stationIp?.trim();
  const setupIP = status.setupIp?.trim() || "192.168.4.1";
  const deviceIP = status.deviceIp?.trim() || (status.stationConnected ? stationIP : setupIP) || setupIP;
  // Once the ESP32 joins the home network, use its station address for all
  // subsequent controls. While in setup mode, keep the AP address visible.
  address.value = `http://${deviceIP}`;
  clientAddress.textContent = deviceIP;
  localStorage.setItem("mini-tv.device-address", address.value);
  const network = status.stationConnected ? `在线 · ${deviceIP}` : `配网热点 · ${deviceIP}`;
  const audio = status.track?.trim() ? `${status.track} · ${status.artist || "未知来源"}` : "未检测到音频";
  statusGrid.innerHTML = `<div class="stat"><span>当前页面</span><strong>${status.mode}</strong></div><div class="stat"><span>设备 IP</span><strong>${deviceIP}</strong></div><div class="stat"><span>网络状态</span><strong>${network}</strong></div><div class="stat"><span>配网热点</span><strong>${status.setupSsid}</strong></div><div class="stat"><span>信号强度</span><strong>${status.stationConnected ? `${status.rssi} dBm` : "等待连接"}</strong></div><div class="stat"><span>音频同步</span><strong>${audio}</strong></div><div class="stat"><span>镜像状态</span><strong>${status.mirrorActive ? `镜像在线 · 已接收 ${status.mirrorFrames} 帧` : "已停止"}</strong></div>`;
}
async function refresh(silent = false) {
  if (statusRefreshInFlight) return;
  statusRefreshInFlight = true;
  if (!silent) showMessage("[CONNECTING] 正在读取设备状态");
  try {
    const status = await DeviceService.status(address.value);
    renderStatus(status);
    setSyncState(status.telemetryValid);
    if (!syncing && autoSync) {
      const result = await DeviceService.startSync(address.value);
      syncing = result.ok;
      syncButton.textContent = syncing ? "停止同步" : "同步电脑";
    }
    if (!silent) showMessage("[ONLINE] 设备已连接");
  } catch (error) { setConnection(false); if (!silent) showMessage(`[ERROR] ${error instanceof Error ? error.message : String(error)}`, true); }
  finally { statusRefreshInFlight = false; }
}
async function scan() { showMessage("正在扫描附近的 Wi-Fi……"); try { const result = await DeviceService.scanNetworks(address.value); networkSelect.innerHTML = "<option value=\"\">选择一个网络</option>"; result.networks.forEach((network: Network) => { const option = document.createElement("option"); option.value = network.ssid; option.textContent = `${network.ssid} · ${network.rssi} dBm${network.secure ? " · 已加密" : " · 开放"}`; networkSelect.append(option); }); showMessage(`扫描完成，发现 ${result.networks.length} 个网络`); } catch (error) { showMessage(error instanceof Error ? error.message : String(error), true); } }
$("#connect").addEventListener("click", () => { void refresh(); }); $("#refresh").addEventListener("click", () => { void refresh(); }); $("#scan-networks").addEventListener("click", scan); networkSelect.addEventListener("change", () => { ssidInput.value = networkSelect.value; });
$("#wifi-form").addEventListener("submit", async (event) => { event.preventDefault(); showMessage("正在保存 Wi-Fi 信息……"); try { await DeviceService.setWiFi(address.value, ssidInput.value, passwordInput.value); showMessage("已保存，设备正在连接家庭 Wi-Fi。请稍后切回家庭网络。"); } catch (error) { showMessage(error instanceof Error ? error.message : String(error), true); } });
document.querySelectorAll<HTMLButtonElement>("[data-mode]").forEach((button) => { button.addEventListener("click", async () => { document.querySelectorAll("[data-mode]").forEach((item) => item.classList.remove("selected")); button.classList.add("selected"); const mode = button.dataset.mode!; if (mode === "virtual") { showMessage("[VIRTUAL] 当前控制端画布已准备好"); return; } showMessage(`[MODE] 正在切换到 ${button.querySelector("b")?.textContent ?? "新页面"}`); try { await DeviceService.setMode(address.value, mode); await refresh(true); } catch (error) { showMessage(`[ERROR] ${error instanceof Error ? error.message : String(error)}`, true); } }); });
syncButton.addEventListener("click", async () => {
  syncing = !syncing;
  try {
    const result = syncing ? await DeviceService.startSync(address.value) : await DeviceService.stopSync(address.value);
    syncButton.textContent = syncing ? "停止同步" : "开始同步";
    syncButton.classList.toggle("selected", syncing);
    showMessage(result.message ?? (syncing ? "电脑状态同步已启动" : "电脑状态同步已停止"));
  } catch (error) { syncing = !syncing; showMessage(error instanceof Error ? error.message : String(error), true); }
});
mirrorButton.addEventListener("click", async () => {
  mirroring = !mirroring;
  try {
    const result = mirroring ? await DeviceService.startMirror(address.value) : await DeviceService.stopMirror(address.value);
    mirrorButton.textContent = mirroring ? "停止镜像" : "启动镜像";
    mirrorButton.classList.toggle("selected", mirroring);
    showMessage(result.message ?? (mirroring ? "镜像已启动" : "镜像已停止"));
    await refresh();
  } catch (error) { mirroring = !mirroring; showMessage(error instanceof Error ? error.message : String(error), true); }
});
autoSyncButton.addEventListener("click", async () => {
  autoSync = !autoSync;
  autoSyncButton.setAttribute("aria-pressed", String(autoSync));
  if (autoSync) {
    syncing = true;
    await DeviceService.startSync(address.value);
    syncButton.textContent = "停止同步";
    showMessage("[SYNC] 自动同步已开启");
  } else {
    syncing = false;
    await DeviceService.stopSync(address.value);
    syncButton.textContent = "同步电脑";
    showMessage("[SYNC] 自动同步已暂停");
  }
});
virtualOpen.addEventListener("click", () => {
  void (async () => {
    if (virtualActive) {
      if (virtualMirroring) {
        try { await DeviceService.stopVirtualMirror(address.value); } catch { /* display shutdown still proceeds */ }
        virtualMirroring = false;
      }
      renderVirtualDisplay(await DeviceService.stopVirtualDisplay());
      showMessage("[VIRTUAL] 系统虚拟屏幕已关闭");
      return;
    }
    showMessage("[VIRTUAL] 正在创建系统虚拟屏幕……");
    try {
      const status = await DeviceService.startVirtualDisplay();
      renderVirtualDisplay(status);
      showMessage(`[VIRTUAL] 已创建显示器 ${status.displayId}，现在可以把窗口拖过去`);
    } catch (error) {
      showMessage(`[ERROR] ${error instanceof Error ? error.message : String(error)}`, true);
    }
  })();
});
virtualMirror.addEventListener("click", () => {
  void (async () => {
    if (!virtualActive) return;
    try {
      const result = virtualMirroring
        ? await DeviceService.stopVirtualMirror(address.value)
        : await DeviceService.startVirtualMirror(address.value);
      virtualMirroring = !virtualMirroring;
      virtualMirror.textContent = virtualMirroring ? "停止发送" : "发送到 ESP32";
      virtualMirror.classList.toggle("selected", virtualMirroring);
      showMessage(result.message ?? (virtualMirroring ? "虚拟屏幕正在发送到 ESP32" : "虚拟屏幕发送已停止"));
    } catch (error) {
      virtualMirroring = false;
      virtualMirror.textContent = "发送到 ESP32";
      virtualMirror.classList.remove("selected");
      showMessage(`[ERROR] ${error instanceof Error ? error.message : String(error)}`, true);
    }
  })();
});
void DeviceService.virtualDisplayStatus().then(renderVirtualDisplay).catch(() => undefined);
syncButton.addEventListener("dblclick", () => { autoSyncButton.click(); });
setConnection(false);
window.setInterval(() => { void refreshComputer(); }, 1500);
window.setInterval(() => { void refresh(true); }, 2500);
window.setTimeout(() => { void refresh(); void refreshComputer(); }, 250);
