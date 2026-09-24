# turing-monitor

> [!WARNING]
> **本项目是非官方的第三方实现，与图灵智显（TURZX）及其关联公司、经销商没有任何
> 关系。** 这是**独立开发的替代软件，不是厂商提供的原始软件**。它会直接操作硬件，请自行评估风险。
>
> **请勿在本仓库提交关于厂商软件或屏幕硬件本身的问题：**
> - 屏幕、官方软件（TURZX.exe 等）相关的问题，请前往[官方论坛](http://discuz.turzx.com/)
> - 其他品牌的屏幕，请联系你的卖家
>
> 另外请注意：**Linux 相关功能尚未在真机上验证过**，详见下方[进度](#进度)表。
>
> **本软件按「原样」提供，不附带任何担保。它不是官方驱动，使用中若因软件实现的
> 局限、操作失误或硬件差异导致屏幕异常乃至损坏，风险与后果由使用者自行承担。**
> 完整条款见[免责声明](#免责声明)。

在 **macOS** 和 **Linux** 上驱动 **图灵智显 / TURZX** USB 小屏幕 —— 不需要内核
驱动、不需要签名、不需要 DKMS。

厂商只提供了 Windows 软件，而且它的驱动针对的是 USB 标识
`1A86:AD10..AD13` —— 而屏幕处于正常的「LCD 模式」时并不会呈现这个标识。所以
在 macOS 和 Linux 上它完全不被识别。本项目直接从用户态与屏幕通信，两端都只
需要 libusb，无需安装其他东西。

你可以用它做这些事：

- **显示图片** —— 任意 JPEG 或 PNG，自动缩放到屏幕尺寸
- **播放视频** —— MP4 或裸 H.264
- **系统监控副屏** —— CPU、内存、网络、磁盘、温度
- **当作第二显示器** —— 可以把真实窗口拖上去

## 支持的型号

| USB ID | 型号 | 分辨率 |
|---|---|---|
| `1cbe:0050` | 图灵 5.2" | 720×1280 |
| `1cbe:0028` | 图灵 2.8" 圆形 | 480×480 |
| `1cbe:0046` | 图灵 4.6" | 320×960 |
| `1cbe:0080` | 图灵 8.0" | 800×1280 |
| `1cbe:0088` | 图灵 8.8" | 480×1920 |
| `1cbe:0092` | 图灵 9.2" | 462×1920 |
| `1cbe:0123` | 图灵 12.3" | 720×1920 |

运行 `turzx info` 可以看到你的屏幕报出的型号。如果没列出，在
`internal/proto/model.go` 的型号表里加一行即可。

## 安装

### macOS

```sh
brew install libusb
git clone https://github.com/jijiechen/turing-monitor
cd turing-monitor
PKG_CONFIG_PATH="$(brew --prefix)/lib/pkgconfig" go build -o turzx ./cmd/turzx
```

需要 Go 1.24 或更高版本。如果你已经装了 Go，这样比 clone 更省事：

```sh
PKG_CONFIG_PATH="$(brew --prefix)/lib/pkgconfig" \
  go install github.com/jijiechen/turing-monitor/cmd/turzx@latest
```

如果你不想装 Go，可以直接从 [Releases](../../releases) 下载预编译二进制。macOS
版本未签名，首次运行需要**右键 → 打开**，或者：

```sh
xattr -d com.apple.quarantine ./turzx
```

### Ubuntu / Debian

```sh
sudo apt install libusb-1.0-0-dev
sudo apt install ffmpeg          # 只有 turzx monitor 需要
git clone https://github.com/jijiechen/turing-monitor
cd turing-monitor
go build -o turzx ./cmd/turzx
```

然后让自己的用户无需 `sudo` 就能访问屏幕：

```sh
sudo cp packaging/linux/99-turzx.rules /etc/udev/rules.d/
sudo udevadm control --reload-rules && sudo udevadm trigger
```

拔掉屏幕再插上。**跳过这一步，所有命令都会报 `libusb: bad access`。**

## 使用

先确认屏幕能被找到：

```sh
./turzx info
# Turing 5.2" (720x1280)  serial 0123456789abcdef  bus 2 addr 1
```

### 显示图片

```sh
./turzx image ~/Pictures/photo.jpg
```

任意尺寸都可以：图片会被缩放到适合屏幕的大小，多出来的部分用黑边补齐。屏幕
自身不做任何缩放，所有适配都由主机完成。

### 播放视频

```sh
./turzx video ~/Movies/clip.mp4          # MP4 会即时解复用
./turzx video clip.h264 --loop           # 裸 Annex-B H.264，循环播放
./turzx video clip.mp4 --fps 24          # 手动指定帧率
```

MP4 解析是内置的，所以**播放视频不需要 ffmpeg**。播放节奏由屏幕自己控制，意味着
整段视频会在远短于其时长的时间内推送完毕、然后继续播放 —— 这是正常的。

### 系统监控副屏

```sh
./turzx dashboard                        # 每秒刷新
./turzx dashboard --interval 500ms
```

显示 CPU、负载、内存、交换分区、网络吞吐、磁盘占用和温度。按 Ctrl-C 停止。

macOS 上不显示温度：Apple Silicon 的温度只能通过 SMC 读取，而未签名的命令行
工具拿不到这个权限。此时布局会自动调整，把空间让给磁盘面板。

### 第二显示器

```sh
./turzx monitor                  # 30 fps，16 Mbit/s
./turzx monitor --bitrate 24000  # 画面更清晰，前提是屏幕跟得上
./turzx monitor --fps 24         # 降低帧率，如果跟不上的话
```

Linux 上需要额外配置、macOS 上需要授权，详见下面的
**[扩展桌面](#扩展桌面)**。

### 其他命令

```sh
./turzx brightness 60     # 背光亮度，0-100
./turzx rotate 1          # 0-3，屏幕竖装时用
./turzx clear             # 清屏
./turzx sync              # 握手，同时打印固件版本
./turzx storage           # SD 卡用量（如果插了卡）
./turzx extract in.mp4 out.h264   # 转换格式，不连接屏幕
```

## 扩展桌面

`turzx monitor` 会创建一块和屏幕同尺寸的显示器，采集它、编码成 H.264 并推流，
于是这块屏就成了真正的第二显示器，可以直接把窗口拖上去。

### macOS

需要授权一次：**系统设置 → 隐私与安全性 → 屏幕录制**，勾选你的终端，然后**重启
终端**。首次运行时 macOS 会主动弹窗询问；没有授权的话，采集会失败并给出明确
提示。

虚拟显示器由本程序通过 CoreGraphics 内部的私有 API `CGVirtualDisplay` 创建 —— 与
DisplayLink、BetterDisplay、DeskPad 用的是同一套机制。有两点需要知道：

- 它是**未公开的 API**，macOS 任何一次更新都可能让它失效
- 因此它无法上架 Mac App Store

### Linux

两个前提条件：

**1. X11 会话。** 登录时选择「Ubuntu on Xorg」。不支持 Wayland：它的合成器没有
提供创建虚拟输出的方式，而且屏幕采集要走 portal 而非 X 屏幕。

**2. 一个已经存在的虚拟输出。** Linux 上用户态程序无法创建显示器，需要先配好。
要么安装 evdi：

```sh
sudo apt install evdi-dkms && sudo modprobe evdi
```

要么在 `xorg.conf` 里加一个 dummy 设备：

```
Section "Device"
  Identifier "Virtual"
  Driver     "dummy"
  VideoRam   32768
  Option     "ConnectedMonitor" "DP-1"
EndSection
```

然后运行 `turzx monitor`。它会自动挑选输出，优先选择尺寸已经和屏幕一致的那个。
如果有多个，用 `--output <名称>` 指定 —— 名称可以从 `xrandr --query` 得到。如果
找不到合适的输出，它会列出当前可用的并告诉你该怎么配。

### 调优

默认参数面向 720×1280 的桌面内容、走 USB 2.0。如果运动中画面发糊，调高
`--bitrate`；如果卡顿，调低 `--fps`。另外注意：画面静止时几乎没有数据流量（屏幕
会一直显示最后一帧），所以帧率在没东西动的时候偏低是正常的。

## 常见问题

**`no TURZX display found`**
屏幕没插，或者处于 desktop 模式。在 Linux 上用 `lsusb` 确认、在 macOS 上看「系统
信息 → USB」，找 `1cbe:` 开头的设备。如果看到的是 `1a86:ad1x`，说明屏幕处于
desktop 模式，本项目不支持。

**`libusb: bad access` / `claim interface failed`**
Linux 上说明少了那条 udev 规则。两个系统上都可能是另一个 `turzx` 进程还在运行、
占着屏幕 —— 同一时间只允许一个进程使用。

**刚杀掉上一个进程，`claim interface 0` 就失败**
操作系统释放接口需要一点时间，等几秒再试。

**扩展桌面报 `Screen Recording permission is not granted`**
见上面的 macOS 小节。授权之后必须重启终端。

**扩展桌面运动中画面发糊、有拖影**
调高 `--bitrate`。如果仍然糊，注意屏幕的硬件解码器只支持 H.264 4:2:0，所以小号
彩色文字会有轻微彩边 —— 这部分无法调整。

**视频播放速度明显偏快**
播放节奏由屏幕根据自己的缓冲区控制。如果屏幕忽略了请求的帧率，用 `--fps` 显式
指定。

## 进度

| | macOS | Linux |
|---|---|---|
| 图片、视频、亮度、旋转 | ✅ 已验证 | ⚠️ 未验证 |
| 系统监控副屏 | ✅ 已验证 | ⚠️ 未验证 |
| 扩展桌面 | ✅ 已验证 | ⚠️ 未验证 |

macOS 已在真实的 5.2" 屏幕上验证过（macOS 26.6.2 / Apple Silicon）。**Linux 部分
代码已完成，但尚未在真机上运行过** —— 预计会遇到需要修的地方，欢迎反馈。

## 实现原理

LCD 模式下的屏幕会暴露一个厂商自定义的 USB 接口，带两个批量端点
（`0x01` OUT、`0x81` IN）。每条消息都是一个 512 字节的块：一个用 DES-CBC 加密的
500 字节命令包，后面可以跟一段原始载荷，比如 JPEG 图片或一段 H.264 数据。

完整的协议格式 —— 包括用于锁定加密实现的黄金测试向量 —— 见
**[docs/PROTOCOL.md](docs/PROTOCOL.md)**。

## 目录结构

```
cmd/turzx/          命令行入口
internal/proto/     协议层：加密、组帧、命令（纯 Go）
internal/usb/       USB 传输与屏幕操作
internal/media/     MP4 → Annex-B H.264 解复用（纯 Go，不需要 ffmpeg）
internal/metrics/   系统指标采集，分平台实现
internal/dashboard/ 把指标渲染成画面并推送
internal/vdisplay/  创建或查找虚拟显示器
internal/capture/   采集显示器并编码为 H.264
docs/PROTOCOL.md    协议参考文档
```

## 开发

```sh
go test ./...
```

用真实文件测试 MP4 解复用：

```sh
TURZX_TEST_MP4=/path/to/video.mp4 go test ./internal/media/ -v
```

## 说明与限制

* 屏幕有**两种 USB 模式**。本项目针对的是 **LCD 模式**（`1cbe:00xx`），也就是屏幕
  开机后默认所处的模式。厂商的 Windows 驱动走的是 **desktop 模式**
  （`1a86:AD1x`），那是另一套协议，本项目未实现。
* 单次 USB 传输必须小于 1 MiB，否则设备会超时；更大的载荷会被自动分片。
* 播放屏幕自身 SD 卡上的文件，协议层已实现，但需要插卡，且未经验证。
* 这是**非官方的第三方实现**，与图灵智显（TURZX）官方无关。

## 致谢

协议的正确性经过
[`turing-smart-screen-python`](https://github.com/mathoudebine/turing-smart-screen-python)
交叉验证，该项目对这一系列设备的参考实现非常有价值。

## 免责声明

**本软件不是官方驱动，按「原样」提供，不附带任何形式的明示或暗示担保。**

它与屏幕之间的通信协议为**独立实现**，未经厂商公开或认可。该实现
虽然在真实设备上验证过，但不同批次、不同固件版本的硬件仍可能存在差异。

因此，**使用本软件可能因软件实现的局限、操作失误或硬件个体差异，导致屏幕工作异
常、数据丢失甚至设备损坏。上述风险及后果由使用者自行承担**，项目作者与贡献者不
对任何直接或间接的损害承担责任。

如果你对自己的设备没有把握，建议：

- 优先使用厂商提供的官方软件
- 先用 `turzx info` 确认型号，再尝试其他命令
- 使用 `turzx dashboard` 这类只读取、只显示的指令，风险最低

完整法律条款见 [LICENSE](LICENSE)（MIT 许可证中的免责条款）。

## 许可证

MIT —— 见 [LICENSE](LICENSE)。
