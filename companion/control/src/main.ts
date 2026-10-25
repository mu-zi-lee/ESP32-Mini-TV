import { DeviceService, type AgentStatusInfo, type AgentStatusSnapshot, type DeviceStatus, type Network, type SerialPort } from "./mygo";

const $ = <T extends HTMLElement>(selector: string) => document.querySelector<T>(selector)!;
const address = $<HTMLInputElement>("#address");
const statusText = $("#status");
const networkSelect = $<HTMLSelectElement>("#network");
const ssidInput = $<HTMLInputElement>("#ssid");
const passwordInput = $<HTMLInputElement>("#password");
const connectionLabel = $("#connection-label");
const modeLabel = $("#device-mode");
const modeNote = $("#selected-mode-note");
const syncButton = $("#sync-computer") as HTMLButtonElement;
const mirrorButton = $("#mirror-toggle") as HTMLButtonElement;
const autoSyncButton = $("#auto-sync") as HTMLButtonElement;
const clientAddress = $("#client-address");
const deviceSubtitle = $("#device-subtitle");
const healthNetwork = $("#health-network");
const healthRssi = $("#health-rssi");
const healthTelemetry = $("#health-telemetry");
const computerTime = $("#computer-time");
const computerDate = $("#computer-date");
const computerCpu = $("#computer-cpu");
const computerMemory = $("#computer-memory");
const computerBattery = $("#computer-battery");
const computerCharging = $("#computer-charging");
const computerAudio = $("#computer-audio");
const computerArtist = $("#computer-artist");
const agentAggregateTag = $("#agent-aggregate-tag");
const agentAggregateDot = $("#agent-aggregate-dot");
const agentAggregateLabel = $("#agent-aggregate-label");
const agentAggregateDetail = $("#agent-aggregate-detail");
const agentUpdated = $("#agent-updated");
const agentList = $("#agent-list");
const agentFileState = $("#agent-file-state");
const agentEventCount = $("#agent-event-count");
const agentEvents = $("#agent-events");
const syncState = $("#sync-state");
const virtualOpen = $("#virtual-open") as HTMLButtonElement;
const virtualMirror = $("#virtual-mirror") as HTMLButtonElement;
const virtualDisplayTag = $("#virtual-display-tag");
const virtualHelp = $("#virtual-help");
const settingsDrawer = $("#settings-drawer");
const drawerBackdrop = $("#drawer-backdrop");
const serialPort = $("#serial-port") as HTMLSelectElement;
const serialHelp = $("#serial-help");
const serialState = $("#flash-device-state");
const flashStart = $("#flash-start") as HTMLButtonElement;
const flashProgress = $("#flash-progress");
const flashTitle = $("#flash-title");
const flashPercent = $("#flash-percent");
const flashLog = $("#flash-log");
let syncing = false;
let mirroring = false;
let mirrorActionInFlight = false;
let virtualActive = false;
let virtualMirroring = false;
let autoSync = true;
let statusRefreshInFlight = false;
let computerRefreshInFlight = false;
let agentRefreshInFlight = false;
let lastAgentSnapshot: AgentStatusSnapshot | undefined;

const configuredAddress = (import.meta.env.MINI_TV_DEVICE_URL ?? import.meta.env.VITE_MINI_TV_DEVICE_URL ?? "").trim();
const savedAddress = localStorage.getItem("mini-tv.device-address")?.trim() ?? "";
address.value = savedAddress || configuredAddress || "http://192.168.4.1";
const configuredSSID = (import.meta.env.MINI_TV_WIFI_SSID ?? import.meta.env.VITE_MINI_TV_WIFI_SSID ?? "").trim();
const configuredPassword = import.meta.env.MINI_TV_WIFI_PASSWORD ?? import.meta.env.VITE_MINI_TV_WIFI_PASSWORD ?? "";
if (configuredSSID) ssidInput.value = configuredSSID;
if (configuredPassword) passwordInput.value = configuredPassword;

address.addEventListener("change", () => localStorage.setItem("mini-tv.device-address", address.value.trim()));
function showMessage(message: string, error = false) { statusText.textContent = message; statusText.className = error ? "notice error" : "notice"; }
function wait(milliseconds: number): Promise<void> { return new Promise((resolve) => window.setTimeout(resolve, milliseconds)); }
function setConnection(connected: boolean) { connectionLabel.textContent = connected ? "已连接" : "未连接"; connectionLabel.parentElement?.classList.toggle("is-live", connected); }
function setSyncState(active: boolean) { syncState.textContent = active ? "LIVE" : "WAIT"; syncState.classList.toggle("is-live", active); }
function renderMirrorButton() { mirrorButton.textContent = mirrorActionInFlight ? "正在处理…" : (mirroring ? "停止镜像" : "开始镜像"); mirrorButton.disabled = mirrorActionInFlight; }
function renderComputer(payload: Awaited<ReturnType<typeof DeviceService.computerStatus>>) {
  computerTime.textContent = payload.time || "--:--";
  computerDate.textContent = payload.date || "等待同步";
  computerCpu.textContent = payload.cpu > 0 ? String(payload.cpu) : "--";
  computerMemory.textContent = payload.memory > 0 ? String(payload.memory) : "--";
  computerBattery.textContent = payload.battery > 0 ? String(payload.battery) : "--";
  computerCharging.textContent = payload.charging ? "充电中" : "%";
  const hasAudio = payload.track.trim().length > 0;
  computerAudio.textContent = hasAudio ? payload.track : "未播放";
  computerArtist.textContent = hasAudio ? `${payload.artist || "未知艺术家"} · ${payload.playing ? "正在播放" : "已暂停"}` : "没有检测到正在运行的播放器";
}
function agentLabel(state: string): string {
  return ({ idle: "已停止", working: "进行中", question: "需要注意", done: "已结束", error: "需要注意" } as Record<string, string>)[state] ?? state;
}
function agentDetail(info: AgentStatusInfo): string {
  const event = info.event || "hook event";
  if (info.message) return `${event} · ${info.message}`;
  return event;
}
function formatAgentTime(timestamp: number): string {
  if (!timestamp) return "--";
  return new Date(timestamp * 1000).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}
function renderAgent(snapshot: AgentStatusSnapshot) {
  lastAgentSnapshot = snapshot;
  const state = snapshot.aggregate || "idle";
  agentAggregateTag.textContent = agentLabel(state);
  agentAggregateTag.className = `status-tag agent-status-tag is-${state}`;
  agentAggregateDot.className = `agent-dot is-${state}`;
  agentAggregateLabel.textContent = agentLabel(state);
  const source = snapshot.aggregateSource ? snapshot.aggregateSource.charAt(0).toUpperCase() + snapshot.aggregateSource.slice(1) : "";
  agentAggregateDetail.textContent = source ? `${source} · ${state === "error" || state === "question" ? "黄灯：需要注意" : state === "working" ? "绿灯：进行中" : "红灯：已停止"}` : "等待 hook 事件";
  agentUpdated.textContent = formatAgentTime(snapshot.updatedAt);
  agentFileState.textContent = snapshot.available ? "状态文件已连接" : "状态文件未检测";
  agentEventCount.textContent = `${snapshot.events.length} 个事件`;
  const sources = Object.keys(snapshot.agents).sort();
  agentList.replaceChildren();
  if (!sources.length) {
    const empty = document.createElement("div"); empty.className = "agent-empty"; empty.textContent = "还没有收到 Codex 或 Claude 的事件"; agentList.append(empty);
  } else {
    for (const source of sources) {
      const info = snapshot.agents[source];
      if (!info) continue;
      const row = document.createElement("div"); row.className = "agent-row";
      const dot = document.createElement("span"); dot.className = `agent-dot is-${info.state}`; dot.setAttribute("aria-hidden", "true");
      const body = document.createElement("div"); body.className = "agent-row-body";
      const name = document.createElement("strong"); name.textContent = source.charAt(0).toUpperCase() + source.slice(1);
      const detail = document.createElement("small"); detail.textContent = `${agentLabel(info.state)} · ${agentDetail(info)}`;
      body.append(name, detail);
      const time = document.createElement("time"); time.textContent = formatAgentTime(info.updatedAt);
      row.append(dot, body, time); agentList.append(row);
    }
  }
  agentEvents.replaceChildren();
  if (!snapshot.events.length) {
    const empty = document.createElement("div"); empty.className = "agent-empty"; empty.textContent = "暂无事件"; agentEvents.append(empty);
  } else {
    for (const event of [...snapshot.events].reverse()) {
      const row = document.createElement("div"); row.className = "agent-event-row";
      const time = document.createElement("time"); time.textContent = formatAgentTime(event.updatedAt);
      const source = document.createElement("strong"); source.textContent = event.source;
      const label = document.createElement("span"); label.textContent = `${event.event || "event"} · ${agentLabel(event.state)}`;
      row.append(time, source, label); agentEvents.append(row);
    }
  }
}
async function refreshAgentStatus() {
  if (agentRefreshInFlight) return;
  agentRefreshInFlight = true;
  try { renderAgent(await DeviceService.agentStatus()); }
  catch { if (!lastAgentSnapshot) renderAgent({ agents: {}, events: [], aggregate: "idle", updatedAt: 0, available: false }); }
  finally { agentRefreshInFlight = false; }
}
function renderStatus(status: DeviceStatus) {
  setConnection(true);
  modeLabel.textContent = status.mode === "HOME" ? "主页" : status.mode === "DASH" ? "系统" : status.mode;
  modeLabel.classList.add("is-live");
  document.querySelectorAll<HTMLButtonElement>("[data-mode]").forEach((button) => {
    button.classList.toggle("selected", button.dataset.mode?.toUpperCase() === (status.mode === "DASH" ? "DASHBOARD" : status.mode));
    button.disabled = status.mirrorActive && button.dataset.mode !== "mirror";
  });
  modeNote.textContent = modeLabel.textContent;
  const stationIP = status.stationIp?.trim();
  const setupIP = status.setupIp?.trim() || "192.168.4.1";
  const deviceIP = status.deviceIp?.trim() || (status.stationConnected ? stationIP : setupIP) || setupIP;
  address.value = `http://${deviceIP}`;
  clientAddress.textContent = deviceIP;
  localStorage.setItem("mini-tv.device-address", address.value);
  deviceSubtitle.textContent = `${deviceIP} · ${status.stationConnected ? "家庭 Wi‑Fi" : "配网热点"}`;
  healthNetwork.textContent = status.stationConnected ? "在线" : "配网热点";
  healthRssi.textContent = status.stationConnected ? `${status.rssi} dBm` : "--";
  healthTelemetry.textContent = status.telemetryValid ? "实时" : "等待同步";
  healthTelemetry.classList.toggle("is-live", status.telemetryValid);
  if (!mirrorActionInFlight) mirroring = status.mirrorActive;
  renderMirrorButton();
}
async function refreshComputer() {
  if (computerRefreshInFlight) return;
  computerRefreshInFlight = true;
  try { renderComputer(await DeviceService.computerStatus()); } catch { computerArtist.textContent = "电脑状态读取失败"; }
  finally { computerRefreshInFlight = false; }
}
async function refresh(silent = false) {
  if (statusRefreshInFlight) return;
  statusRefreshInFlight = true;
  if (!silent) showMessage("正在读取设备状态…");
  try {
    const configured = address.value.trim();
    const candidates = [configured, "http://mini-tv.local", "http://192.168.4.1"].filter((value, index, list) => value && list.indexOf(value) === index);
    let status: DeviceStatus | undefined;
    let connectedCandidate = configured;
    // ESP32 may need a few seconds after reset to associate with the saved
    // router and register mini-tv.local. Retry the small candidate set here so
    // a normal reboot is shown as "waiting" instead of a false hard failure.
    for (let attempt = 0; attempt < 4 && !status; attempt++) {
      for (const candidate of candidates) {
        try { status = await DeviceService.status(candidate); connectedCandidate = candidate; break; } catch { /* try next address */ }
      }
      if (!status && attempt < 3) {
        if (!silent) showMessage(`正在等待 ESP32 上线…（${attempt + 1}/3）`);
        await wait(700 + attempt * 350);
      }
    }
    if (!status) throw new Error("无法找到 ESP32，请检查地址和 Wi‑Fi");
    if (connectedCandidate !== configured) address.value = connectedCandidate;
    renderStatus(status);
    setSyncState(status.telemetryValid);
    if (!syncing && autoSync) { try { await DeviceService.startSync(address.value); syncing = true; syncButton.textContent = "停止同步"; } catch { syncing = false; } }
    showMessage("设备已连接");
  } catch (error) {
    setConnection(false);
    modeLabel.textContent = "离线";
    modeLabel.classList.remove("is-live");
    if (!silent) showMessage("ESP32 暂未上线，请确认 Mac 已连接同一个 Wi‑Fi；Control 会继续重试", true);
  }
  finally { statusRefreshInFlight = false; }
}
async function scan() { showMessage("正在扫描附近的 Wi‑Fi…"); try { const result = await DeviceService.scanNetworks(address.value); networkSelect.innerHTML = "<option value=\"\">选择网络</option>"; result.networks.forEach((network: Network) => { const option = document.createElement("option"); option.value = network.ssid; option.textContent = `${network.ssid} · ${network.rssi} dBm`; networkSelect.append(option); }); showMessage(`扫描完成，发现 ${result.networks.length} 个网络`); } catch (error) { showMessage(error instanceof Error ? error.message : String(error), true); } }
async function scanSerialPorts() { serialState.textContent = "扫描中"; serialHelp.textContent = "正在查找连接到 Mac 的 ESP32…"; try { const ports: SerialPort[] = await DeviceService.listSerialPorts(); serialPort.innerHTML = "<option value=\"\">选择 ESP32 串口</option>"; ports.forEach((item) => { const option = document.createElement("option"); option.value = item.port; option.textContent = item.description ? `${item.port} · ${item.description}` : item.port; serialPort.append(option); }); serialState.textContent = ports.length ? `${ports.length} 个设备` : "未找到"; serialHelp.textContent = ports.length ? "选择设备后即可开始烧录。" : "请检查 USB 数据线，或按住 BOOT 键后重新扫描。"; flashStart.disabled = ports.length === 0; } catch (error) { serialState.textContent = "扫描失败"; serialHelp.textContent = error instanceof Error ? error.message : String(error); flashStart.disabled = true; } }
async function flash() { if (!serialPort.value) return; flashStart.disabled = true; flashProgress.hidden = false; flashTitle.textContent = "正在编译并烧录"; flashPercent.textContent = "进行中"; flashLog.textContent = ""; try { const result = await DeviceService.flashFirmware(serialPort.value); flashTitle.textContent = "烧录完成"; flashPercent.textContent = "完成"; flashLog.textContent = result.message ?? "固件已写入设备"; serialState.textContent = "已完成"; showMessage("固件已写入 ESP32，等待设备重启"); } catch (error) { flashTitle.textContent = "烧录失败"; flashPercent.textContent = "需要重试"; flashLog.textContent = error instanceof Error ? error.message : String(error); serialState.textContent = "失败"; showMessage("烧录失败，请查看详细日志", true); } finally { flashStart.disabled = false; } }
function openDrawer() { settingsDrawer.classList.add("is-open"); settingsDrawer.setAttribute("aria-hidden", "false"); drawerBackdrop.hidden = false; }
function closeDrawer() { settingsDrawer.classList.remove("is-open"); settingsDrawer.setAttribute("aria-hidden", "true"); drawerBackdrop.hidden = true; }

document.querySelectorAll<HTMLButtonElement>("[data-view]").forEach((button) => button.addEventListener("click", () => { document.querySelectorAll<HTMLButtonElement>("[data-view]").forEach((item) => item.classList.toggle("is-active", item === button)); document.querySelectorAll<HTMLElement>(".workspace").forEach((view) => { view.hidden = view.id !== button.dataset.view; view.classList.toggle("is-active", view.id === button.dataset.view); }); if (button.dataset.view === "prep-view") void scanSerialPorts(); }));
$("#connect").addEventListener("click", () => void refresh()); $("#refresh").addEventListener("click", () => void refresh()); $("#scan-networks").addEventListener("click", () => void scan()); $("#network-open").addEventListener("click", openDrawer); $("#settings-open").addEventListener("click", openDrawer); $("#settings-close").addEventListener("click", closeDrawer); drawerBackdrop.addEventListener("click", closeDrawer);
networkSelect.addEventListener("change", () => { ssidInput.value = networkSelect.value; });
$("#wifi-form").addEventListener("submit", async (event) => { event.preventDefault(); showMessage("正在保存 Wi‑Fi 信息…"); try { await DeviceService.setWiFi(address.value, ssidInput.value, passwordInput.value); showMessage("已保存，设备正在连接家庭 Wi‑Fi"); } catch (error) { showMessage(error instanceof Error ? error.message : String(error), true); } });
document.querySelectorAll<HTMLButtonElement>("[data-mode]").forEach((button) => button.addEventListener("click", async () => { const mode = button.dataset.mode!; try { await DeviceService.setMode(address.value, mode); await refresh(true); showMessage(`已切换到${button.querySelector("b")?.textContent ?? "新页面"}`); } catch (error) { showMessage(error instanceof Error ? error.message : String(error), true); } }));
syncButton.addEventListener("click", async () => { syncing = !syncing; try { const result = syncing ? await DeviceService.startSync(address.value) : await DeviceService.stopSync(address.value); syncButton.textContent = syncing ? "停止同步" : "开始同步"; setSyncState(syncing); showMessage(result.message ?? (syncing ? "电脑状态同步已启动" : "电脑状态同步已停止")); } catch (error) { syncing = !syncing; showMessage(error instanceof Error ? error.message : String(error), true); } });
mirrorButton.addEventListener("click", async () => { if (mirrorActionInFlight) return; const next = !mirroring; mirrorActionInFlight = true; mirroring = next; renderMirrorButton(); try { const result = next ? await DeviceService.startMirror(address.value) : await DeviceService.stopMirror(address.value); showMessage(result.message ?? (next ? "镜像已启动" : "镜像已停止")); } catch (error) { mirroring = !next; showMessage(error instanceof Error ? error.message : String(error), true); } finally { mirrorActionInFlight = false; renderMirrorButton(); await refresh(true); } });
autoSyncButton.addEventListener("click", async () => { autoSync = !autoSync; autoSyncButton.setAttribute("aria-pressed", String(autoSync)); try { if (autoSync) { await DeviceService.startSync(address.value); syncing = true; syncButton.textContent = "停止同步"; } else { await DeviceService.stopSync(address.value); syncing = false; syncButton.textContent = "开始同步"; } showMessage(autoSync ? "自动同步已开启" : "自动同步已暂停"); } catch (error) { showMessage(error instanceof Error ? error.message : String(error), true); } });
virtualOpen.addEventListener("click", () => { void (async () => { if (virtualActive) { if (virtualMirroring) { try { await DeviceService.stopVirtualMirror(address.value); } catch { /* display shutdown still proceeds */ } virtualMirroring = false; } const status = await DeviceService.stopVirtualDisplay(); virtualActive = status.active; virtualDisplayTag.textContent = "未创建"; virtualHelp.textContent = "创建系统级显示器后，可以把任意窗口拖过去。"; virtualOpen.textContent = "创建虚拟屏幕"; virtualMirror.disabled = true; showMessage("虚拟屏幕已关闭"); return; } showMessage("正在创建系统虚拟屏幕…"); try { const status = await DeviceService.startVirtualDisplay(); virtualActive = status.active; virtualDisplayTag.textContent = "在线"; virtualDisplayTag.classList.add("active"); virtualOpen.textContent = "关闭虚拟屏幕"; virtualMirror.disabled = false; virtualHelp.textContent = "系统显示器已创建，可以拖动窗口到那里。"; showMessage("虚拟屏幕已创建"); } catch (error) { showMessage(error instanceof Error ? error.message : String(error), true); } })(); });
virtualMirror.addEventListener("click", () => { void (async () => { try { const result = virtualMirroring ? await DeviceService.stopVirtualMirror(address.value) : await DeviceService.startVirtualMirror(address.value); virtualMirroring = !virtualMirroring; virtualMirror.textContent = virtualMirroring ? "停止发送" : "发送到 ESP32"; showMessage(result.message ?? (virtualMirroring ? "虚拟屏幕正在发送" : "虚拟屏幕发送已停止")); } catch (error) { showMessage(error instanceof Error ? error.message : String(error), true); } })(); });
$("#serial-scan").addEventListener("click", () => void scanSerialPorts()); $("#prep-scan").addEventListener("click", () => void scanSerialPorts()); serialPort.addEventListener("change", () => { flashStart.disabled = !serialPort.value; }); flashStart.addEventListener("click", () => void flash());
setConnection(false); void DeviceService.virtualDisplayStatus().then((status) => { virtualActive = status.active; virtualDisplayTag.textContent = status.active ? "在线" : "未创建"; virtualDisplayTag.classList.toggle("active", status.active); virtualOpen.textContent = status.active ? "关闭虚拟屏幕" : "创建虚拟屏幕"; virtualMirror.disabled = !status.active; }).catch(() => undefined);
// Computer telemetry is intentionally human-scale. The backend also caches
// samples so sync and the dashboard never launch duplicate system probes.
window.setInterval(() => { void refreshComputer(); }, 3000);
window.setInterval(() => { void refresh(true); }, 5000);
window.setInterval(() => { void refreshAgentStatus(); }, 500);
window.setTimeout(() => { void refresh(); void refreshComputer(); void refreshAgentStatus(); }, 250);
