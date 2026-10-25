# ESP32 Mini TV

ESP32 Mini TV 是一个开源的桌面小电视：由 ESP32 DevKit V1 驱动 1.8 英寸
ST7735 TFT，在 160×128 的横向画布上显示状态、音乐信息、设备监控和电脑镜像。
macOS 端的 **Control** 应用负责配网、同步电脑状态、采集屏幕，并可以创建一个真正的
macOS 系统级虚拟显示器，把拖到虚拟显示器上的窗口发送到 ESP32。

这是一个面向个人桌面和局域网的 DIY 项目，不是 AirPlay 接收器，也不是原生的 macOS
扩展显示器。ESP32 是无线画面查看器，采集、缩放、JPEG 编码和系统集成都在 Mac 上完成。

## 功能概览

- 1.8 英寸 128×160 ST7735 屏幕横向显示，逻辑画布固定为 **160×128**。
- 离线优先的主页、镜像、音乐、设备监控、设置和错误页面。
- ESP32 同时保留 `MiniTV-Setup-XXXX` 配网热点，并自动重连已保存的家庭 Wi-Fi。
- macOS Control 应用提供中文界面、Wi-Fi 扫描/保存、设备状态和模式切换。
- 同步 Mac CPU、内存、电池、充电状态以及当前已播放的音频信息；Control 只读取已经运行的播放器，不会替用户启动音乐应用。
- 电脑镜像：通过常驻 FFmpeg/AVFoundation 采集屏幕，缩放到 160×128 后以 JPEG/UDP 发送。
- 系统虚拟屏幕：创建 macOS `CGVirtualDisplay`，把任意拖到该屏幕的窗口发送到 ESP32。
- 镜像采用“最新帧优先”策略，限制数据包大小，避免网络堵塞时延迟不断累积；高刷新实验档约每 30ms 最多提交一帧，静止画面只发送低频心跳。

## 项目状态

当前仓库是可运行的开发版。PlatformIO 固件、ST7735 初始化、固定引脚、横屏布局、HTTP
配网接口、JPEG/UDP 接收以及 macOS Control 的主要流程已经实现并在当前硬件上验证过。

## 硬件与接线

### 需要的东西

- ESP32 DevKit V1（`esp32dev`，当前测试芯片为 ESP32-D0WDQ6，4 MB Flash）
- 1.8 英寸 ST7735 TFT，分辨率 128×160，SPI 接口
- 杜邦线和一根可传数据的 USB 线
- macOS 电脑（用于 Control、屏幕采集和可选的系统虚拟屏幕）

### TFT 与 ESP32 引脚

下表是固件已经验证的接线。屏幕上的 `SDA` 在这里表示 SPI 的 MOSI 数据线，**不是 I2C SDA**。

| TFT 模块标记 | ESP32 DevKit | GPIO | 说明 |
| --- | --- | ---: | --- |
| `CS` | `D27` | 27 | SPI 片选 |
| `DC` / `A0` | `D26` | 26 | 数据/命令选择 |
| `RST` / `RES` | `D25` | 25 | 屏幕复位 |
| `GND` | `GND` | - | 必须共地 |
| `VCC` | `3V3` | - | 使用 3.3 V 供电 |
| `SCL` / `SCLK` | `D18` | 18 | SPI 时钟 |
| `SDA` / `MOSI` | `D23` | 23 | SPI 数据 |
| `BLK` / `LED` | 取决于模块 | - | 按屏幕模块数据手册接背光 |

不要把 TFT 的 `VCC` 接到 5 V；所有电源和信号地必须共地。`BLK` 背光接法取决于
模块设计，有些模块已经带限流电阻；固件不假设它可以软件调光，接线前请确认模块丝印和数据手册。
`MISO/SDO` 当前不使用，可以悬空。物理面板仍为 128×160，`setRotation(1)` 仅将逻辑画布
旋转为横屏 160×128，不会增加面板分辨率。

## 从零开始

### 1. 安装工具

固件端需要：

- [PlatformIO](https://platformio.org/)（VS Code 的 PlatformIO IDE 或 PlatformIO Core）
- ESP32 板卡可用的 USB 串口驱动（部分 USB-UART 芯片需要额外驱动）

Control 需要可运行 MyGo 和系统级屏幕采集的 macOS，以及：

- Go 1.27+
- [Bun](https://bun.sh/)
- Xcode Command Line Tools（用于编译原生虚拟屏幕助手）
- [FFmpeg](https://ffmpeg.org/)（普通电脑镜像模式使用）

安装 Xcode Command Line Tools：

```sh
xcode-select --install
```

### 2. 编译并烧录 ESP32 固件

在仓库根目录执行：

```sh
pio run
pio device list
pio run -t upload --upload-port /dev/cu.usbserial-0001
pio device monitor --port /dev/cu.usbserial-0001 --baud 115200
```

把 `/dev/cu.usbserial-0001` 换成 `pio device list` 中实际出现的端口。上传卡在
`Connecting...` 时，按住开发板 `BOOT`，开始连接后松开。成功启动后屏幕会短暂显示红、绿、蓝
三色测试，然后进入主页；串口会打印热点名称和设备地址。

固件支持以下串口命令，每条命令单独一行：

```text
home       mirror       music
dashboard  settings     error
next
```

### 3. 配置首次 Wi-Fi

有两种方式。二选一即可，设备中通过本地 API 保存的凭据优先级更高。

**编译时默认值（可选）**

```sh
cp .env.example .env
# 编辑 .env，填写 MINITV_WIFI_SSID 和 MINITV_WIFI_PASSWORD
pio run
```

`.env` 和生成的 `include/wifi_config.h` 都被 Git 忽略，不要提交真实密码。

**本地配网热点（推荐）**

1. 用 Mac 或手机连接串口输出的 `MiniTV-Setup-XXXX` 热点，默认密码是 `minitv-setup`。
2. 打开 `http://192.168.4.1`，或在 Control 中把设备地址暂时填为该地址。
3. 扫描并保存家庭 Wi-Fi 的 SSID 和密码。
4. ESP32 会继续保留配网热点并尝试连接家庭 Wi-Fi；让 Mac 回到同一个家庭网络。
5. 在 Control 中使用状态面板显示的 `deviceIp`（也可以使用 `http://mini-tv.local`，前提是
   当前网络支持 mDNS）。

当前配网 API 只适合可信的本地网络，尚未提供账户鉴权。不要把设备热点、HTTP 端口或 UDP
端口暴露到公网。

## macOS Control

Control 源码位于 [`companion/control`](companion/control)。它由 Go 服务、MyGo 桌面壳、
Vite/TypeScript 前端和一个可选的 Objective-C 原生虚拟屏幕助手组成。

### 下载发行版

带版本号的 Control 安装包会发布在 GitHub Releases 中。macOS 提供通用架构 `.dmg`，
Windows 提供 `.exe`（Windows runner 可用 NSIS 时还会附带安装器），Linux 提供 `.deb`
和 `.tar.gz`，另外还有一个可部署的浏览器版本。
Windows、Linux 和浏览器版本支持普通屏幕镜像；系统级虚拟屏幕依赖 macOS 的
`CGVirtualDisplay`，只在 macOS Control 中提供。ESP32 固件仍需使用 PlatformIO 单独
编译和烧录，桌面安装包不会携带 PlatformIO 或 Wi-Fi 密码。

```sh
cd companion/control
bun install
cp .env.example .env
```

编辑 `.env` 设置本机默认值：

```dotenv
MINI_TV_DEVICE_URL=http://192.168.4.1
MINI_TV_WIFI_SSID=
MINI_TV_WIFI_PASSWORD=
MINI_TV_SCREEN_INDEX=3
```

`MINI_TV_DEVICE_URL` 是 Control 首次请求的地址；设备成功入网后，Control 会根据
`/api/status` 返回的 station IP 自动更新地址。`MINI_TV_SCREEN_INDEX` 是 FFmpeg 的
AVFoundation 屏幕输入编号。连接了外接显示器时，可以用下面的命令查看编号：

```sh
ffmpeg -f avfoundation -list_devices true -i ''
```

开发和打包：

```sh
bun run generate     # 重新生成 MyGo 类型绑定（需要时执行）
bun run dev          # 开发模式
bun run build        # 编译原生助手并打包 Control 应用
go test ./...
go build ./...
```

打包后的 `.app` 位于 `companion/control/build/` 下对应平台目录。第一次使用普通电脑镜像
时，请在 **系统设置 → 隐私与安全性 → 屏幕录制** 中允许 Control（或其终端进程）采集屏幕。

### 使用系统虚拟屏幕

1. 打开 Control，点击“创建虚拟屏幕”。
2. 在 macOS **系统设置 → 显示器** 中找到新显示器，并按需要排列它。
3. 把任意窗口拖到这个虚拟显示器上。
4. 回到 Control，点击“发送到 ESP32”。
5. 停止时先点击“停止发送”，再点击“关闭虚拟屏幕”。

虚拟显示器不是 Control 窗口内的预览画布，而是 macOS 的系统级显示器。Control 的原生助手
通过 `CGDisplayStream` 取得画面，Go 将 BGRA 帧转换为 160×128 JPEG，再复用 ESP32 的
UDP 镜像通道。创建失败时通常是 macOS 版本、权限或当前应用签名限制导致的。

## Agent Status hook

Control 可以读取 Codex 或 Claude 的生命周期 hook，并把两个来源的最高优先级状态发送给
镜像中的交通灯。状态语义是：**绿灯 = 进行中**（`working`），**红灯 = 停止/结束**
（`done` 或 `idle`），**黄灯 = 有问题，需要注意**（`question` 或 `error`）。

仓库内已经包含可直接执行的 hook：

```sh
export MINI_TV_AGENT_SOURCE=codex
export MINI_TV_AGENT_STATE=working
companion/control/scripts/agent-status-hook.sh
```

它默认写入 `~/.config/mini-tv/agent-status.json`。如果 Control 和 hook 使用不同的 Home
目录，可以设置 `MINI_TV_AGENT_STATUS_FILE`。要在 Codex 中安装说明 skill：

```sh
mkdir -p ~/.codex/skills/mini-tv-agent-status
cp companion/control/skills/agent-status/SKILL.md ~/.codex/skills/mini-tv-agent-status/
```

然后把 hook 命令加入 `~/.codex/hooks.json`，在 Codex 中运行 `/hooks` 审核并信任它，再重启
当前会话。完整发布说明和跨平台构建产物见 [`RELEASE_TEMPLATE.md`](RELEASE_TEMPLATE.md)。

## 项目使用的技术

| 层次 | 技术/库 | 用途 |
| --- | --- | --- |
| 固件 | ESP32 Arduino + PlatformIO | 编译、上传和运行时框架 |
| 屏幕 | Adafruit GFX、Adafruit ST7735/ST7789 | ST7735 初始化、绘图和文字 |
| JPEG | Bodmer `TJpg_Decoder` | 在 ESP32 上解码镜像帧 |
| 网络 | Arduino WiFi、WebServer、Preferences、mDNS | SoftAP/STA 配网、HTTP API、持久化和局域网发现 |
| 控制面 | Go、MyGo 0.6.3、Vite、TypeScript、Bun | macOS Control 桌面应用 |
| 电脑镜像 | FFmpeg AVFoundation | 采集 Mac 屏幕并输出 MJPEG |
| 虚拟屏幕 | Cocoa、CoreGraphics、IOSurface、`CGVirtualDisplay`、`CGDisplayStream` | 创建系统显示器并读取其帧 |
| 图像处理 | Go `golang.org/x/image`、JPEG 编码 | 裁剪、缩放、编码和镜像传输 |
| 传输 | HTTP/JSON + UDP | 控制/状态消息与低延迟图像通道分离 |

### 数据流

```text
Mac Control
  ├─ HTTP/JSON ───────────────► ESP32（状态、模式、Wi-Fi、遥测）
  ├─ JPEG/UDP :8765 ──────────► ESP32（160×128 镜像帧）
  └─ Unix socket VDF1 ◄─────── 原生虚拟屏幕助手

ESP32
  ├─ ST7735 SPI TFT（160×128 横屏）
  └─ MiniTV-Setup-XXXX SoftAP + 家庭 Wi-Fi STA
```

控制消息使用 HTTP JSON。镜像帧不放进 JSON，而是拆成不超过 MTU 的 UDP 数据包；接收端
丢弃过期和不完整帧，只绘制最新的完整 JPEG。桌面静止时会发送低频心跳，但不会重复解码和刷屏，
这样可以避免静止桌面时出现卡顿或无意义的整屏刷新。画面变化较小时，Control 会将变化区域切成
`32×32` 的 JPEG 局部块；首帧和关键帧也使用同一套区块协议。ESP32 会先把一批区块解码到离屏画布，
全部收齐后再用一次连续 SPI 扫描提交，避免逐块开窗造成可见撕裂。若某批 UDP 丢包，接收端保持上一张
完整画面，直到下一次完整关键帧恢复。每隔约 3 秒发送一次完整关键帧，用于从局部块缺失中恢复，
不会留下永久残影。HTTP 控制接口包括 `GET /api/status`、
`GET /api/scan`、`POST /api/wifi`、`POST /api/mode`、`POST /api/telemetry` 和
`POST /api/mirror`。镜像使用 UDP `8765`，完整帧格式由 `MT` 标识、版本号、帧序号、160×128
尺寸、分片序号/总数、数据长度和最多 1186 字节 JPEG 数据组成；局部块在同一头部后增加
`x、y、width、height` 四个字节，单包 JPEG 载荷最多 1182 字节。

整屏变化时默认使用 JPEG：对 160×128 小画面来说，JPEG 通常只需几 KB，动态场景发送更快。
如果局域网稳定、且更看重跳过 ESP32 解码带来的锐度，可以在 Control 启动前设置
`MINI_TV_RAW_FULL_FRAME=1` 切换到 RGB565 直传（每帧 40960 字节、约 35 个 UDP 分片）。
局部 JPEG 刷新和静止心跳不受这个开关影响。

## 排错

| 现象 | 检查项 |
| --- | --- |
| 上传找不到串口 | 换一根数据线，确认 `pio device list`，检查 USB-UART 驱动；必要时按住 `BOOT` 上传 |
| 屏幕全黑 | 先看串口是否通过三色测试；确认 `VCC→3V3`、共地和 `BLK/LED` 接法；检查 CS/DC/RST/SCLK/MOSI 是否按表连接 |
| 画面方向或尺寸错误 | 保持 `setRotation(1)` 和逻辑尺寸 160×128，不要把物理 128×160 改成 160×128 的初始化参数 |
| Control 连不上设备 | 先在浏览器打开 `http://设备地址/api/status`；检查 Mac 与 ESP32 是否在同一 Wi-Fi；配网阶段使用 `192.168.4.1` |
| Wi-Fi 一直未连接 | 重新扫描 SSID，确认 2.4 GHz 网络和密码；保留配网热点并查看串口的重试日志 |
| 镜像显示 `NO SIGNAL` | 开启屏幕录制权限，确认 FFmpeg 屏幕编号；虚拟屏幕模式先创建显示器，再点“发送到 ESP32” |
| 镜像颜色异常/卡顿 | 确认只有一个镜像来源在发送；停止旧的普通镜像后再启动虚拟屏幕镜像；检查 Wi-Fi 信号和 UDP 8765 是否被局域网隔离 |
| 虚拟屏幕创建失败 | 确认使用 macOS、已安装 Xcode Command Line Tools，并从 `bun run build` 产生包含原生助手的 `.app` |
| 音乐/CPU 没有更新 | Control 必须保持运行并能访问设备；播放器需要已经在 Mac 上播放；这部分数据通过每 3 秒一次的遥测同步 |

## 目录结构

```text
src/                       ESP32 固件、显示驱动、页面、网络和镜像接收
include/                   PlatformIO 生成的 Wi-Fi 配置头（不提交真实凭据）
companion/control/         Go + MyGo macOS Control 应用
companion/control/native/  CGVirtualDisplay 原生助手
docs/                      硬件、架构、协议、开发和路线图
protocol/                  控制协议示例
scripts/                   PlatformIO 配置生成和辅助脚本
```

更详细的说明直接放在本 README 的“硬件与接线”“项目使用的技术”和“macOS Control”章节中。

## 安全与隐私

不要提交 `.env`、Wi-Fi 密码、Token、Cookie 或私钥。Wi-Fi 凭据只会进入本地忽略文件、ESP32
Preferences 或 Control 的表单请求，不会写入镜像帧。当前 HTTP 配网接口没有鉴权，因此只在
可信局域网使用，不要端口转发到公网。

## 许可证

本项目采用 [MIT License](LICENSE)。第三方库各自遵循其上游许可证；分发修改后的版本时请
保留对应的版权和许可证声明。
