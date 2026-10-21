# pingtunnel 工作台

给 [pingtunnel](https://github.com/esrrhs/pingtunnel)（ICMP 隧道）做一个能长期挂着的桌面界面：状态、延迟曲线、会话日志、配置分组折叠，一次运行看得清。

> 文档里出现的 `<SERVER_IP>`、`<KEY>`、`<CLIENT_IP>` 都是脱敏占位符，实际值只存在于你自己机器与服务器的配置里。

界面用 [MyGo](https://mygo.egoist.dev/)（Go + 系统 WebView）重写，**不内置隧道协议实现**：它驱动你本机的 `pingtunnel.exe`，参数、日志、统计都从那个进程来。

- 应用主体：`pingtunnel-workbench/`（Go 后端 + TypeScript 前端）
- 可运行产物：`pingtunnel-workbench/build/windows-amd64/`

截图都是实测抓的：

| 文件 | 内容 |
| --- | --- |
| `docs/screenshot-light.png` / `screenshot-dark.png` | 运行中，真实 RTT 与延迟曲线 |
| `docs/screenshot-outbound.png` | 右侧常驻「出口」面板，开关被拒绝时的提示 |
| `docs/screenshot-solved.png` | 根因修复后：RTT 136ms、每秒一个 pong |
| `docs/screenshot-port-busy.png` | 端口被其他程序占用时的可读报错 |

## 为什么是"调 exe"而不是链接库

先做的是链接 `github.com/esrrhs/pingtunnel` 库的版本，实测撞上一个真实的不兼容：

- 上游 2026 重构后，客户端 `processPacket` 第一行是 `if packet.my.Rproto >= 0 { return }`，把"没有 `rproto=-1` 标记"的回包统一当请求丢掉；
- 你服务器跑的是 `jiqing112/pingtunnel`（2023 分叉），回包标记与上游新客户端预期不一致。

让 exe 决定协议之后，界面就与版本解耦：服务器跑哪个版本，本机放哪个版本的 exe 即可，不用跟着改界面。

界面对 exe 的适配方式：启动时跑一次 `pingtunnel.exe -h`，解析它认识哪些参数，再按需拼命令行 —— 旧版没有 `-congestion`、新版没有 `-s5filter`，都不会被误传导致进程直接退出。

## 快速开始

1. `pingtunnel-workbench/build/windows-amd64/` 里两个文件放在同一目录：
   - `pingtunnel-workbench.exe`（本程序）
   - `pingtunnel.exe`（隧道客户端；本包自带一份与你服务器同源的构建，可直接换成任意版本）

   源码树里 `pingtunnel-workbench/bin/pingtunnel.exe` 是客户端的备份，`mygo build` 会重建 `build/`，重建后把它拷回去即可。
2. 右键 `pingtunnel-workbench.exe` → **以管理员身份运行**（Windows 上 ICMP 原始套接字需要管理员权限）。
3. 填服务器、本地端口、转发目标、密钥 → 「启动隧道」。
4. 出现延迟数字与曲线即通；关窗口会收进托盘，隧道继续跑。

「出口」面板里的两个开关只有隧道运行时可点；点开时会先做一次 socks5 连通测试，没通过就拒绝并在页面顶部给出原因，而不是把开关做成一个永远灰着的按钮。

“服务器还没有回包”的排查卡默认不弹：状态行只留一行琥珀色可点的字（“已 22s 没有回包 · 排查”），点开才出现浮层，✕ 收起；浮层不参与排版，不会把表单或摘要卡顶下去。

找不到 `pingtunnel.exe` 时界面会提示，也可以在「日志与高级 → 客户端」里点「选择…」指定路径。

## 界面 ↔ 命令行

| 界面 | 参数 | 备注 |
| --- | --- | --- |
| 服务器 | `-s` | 只填主机，ICMP 没有端口 |
| 本地端口 | `-l` | 客户端监听 `127.0.0.1:<端口>` |
| 转发目标 | `-t` | socks5 模式下不需要 |
| 超时 | `-timeout` | 秒 |
| 密钥 | `-key` | 与服务端一致 |
| socks5 | `-sock5` | 开启后客户端即本地 socks5 代理 |
| 代理用户 / 密码 | `-s5user` / `-s5pass` | |
| 国家过滤 / GeoIP 文件 | `-s5filter` / `-s5ftfile` | 你分叉里的 socks5 国家过滤 |
| 最大连接 | `-maxconn` | |
| 拥塞控制 | `-congestion` | 新版才有，旧版自动跳过 |
| 系统代理 | — | 把 Windows 系统代理指向本程序的本地 HTTP 端口 |
| HTTP 端口 | — | 本地 HTTP→SOCKS5 桥的监听端口，默认 34457 |
| TCP 模式及其调优 | `-tcp` `-tcp_bs` `-tcp_mw` `-tcp_rst` `-tcp_gz` `-tcp_stat` | |
| 日志级别 | `-loglevel` | 只影响界面里的会话日志过滤 |
| ICMP 绑定 | `-icmp_l` | |
| 加密 / 口令 | `-encrypt` / `-encrypt-key` | 新版才有 |

## 文件位置

- 设置：`%APPDATA%\pingtunnel-workbench\settings.json`
- 本次会话：同目录 `session.json` —— 就是 `-c` 用的那份 JSON，可直接在命令行复现同一次会话
- 会话日志只活在窗口里，不落盘（exe 始终以 `-nolog 1` 启动，避免在程序目录堆 `.log`）

## TUN 全局模式（实验性）

真正把整台机器送进隧道：wintun 虚拟网卡 + 用户态 TCP/IP 栈（gVisor netstack）+ 分流默认路由。开关同样在右侧「出口」面板，**默认关闭**。

开启时必须满足：

1. 当前是 socks5 模式且隧道在跑；
2. **数据通路实测通过**：程序会用 socks5 真去 CONNECT 一个外部地址，连不上就直接拒绝并告诉你原因（ping 有回包不算，回显可能是边缘设备代答的）；
3. 管理员权限 + 程序目录里有 `wintun.dll`（已随包）。

开启后做的事：创建名为 `pingtunnel` 的网卡（`198.18.0.1/15`，避让常见网段），先给服务器 IP、`10/8`、`172.16/12`、`192.168/16`、`127/8` 加旁路路由（防止隧道自己的包被送进隧道），再加 `0.0.0.0/1` 与 `128.0.0.0/1` 两条半默认路由接管其余流量 —— 不删除你自己的默认路由，所以关掉时能干净还原。

三道保险：

- **看门狗**：连续 20 秒没有 pong 就自动摘掉 TUN 并恢复路由，不让卡顿变成“整机像坏了”；
- 停隧道 / 退出程序同样关闭并还原；
- 任何一步失败（建网卡、加路由、建协议栈）都会回滚已加的改动。

已知限制：MTU 默认 1400（ICMP 隧道封包开销大，太大容易分片）；DNS 仍走系统解析器，没有劫持；UDP 依赖服务端 socks5 的 ASSOCIATE 支持。

## 一键编译

项目根目录的 `build.bat`：

```bat
build.bat
```

它依次做：检测 Go/Bun（优先用仓库里的 `.toolchain\go`）→ 缺依赖时 `bun install` → `bun run build`（自动重新生成 TS 绑定再打包）→ 把 `pingtunnel.exe`、`wintun.dll` 拷到输出目录。产物在 `pingtunnel-workbench\build\windows-amd64\`。

## 常见故障

### 启动就失败：「客户端已退出」/ `bind: Only one usage of each socket address`

说明本地端口被占了。程序现在会分两种情况处理：

- **占用者是本程序上次留下的客户端**（同一个 `pingtunnel.exe` 路径）→ 自动接管，日志里会出现「已清理上次残留的客户端进程（PID x）」，然后正常启动。这种情况多发生在应用被任务管理器强杀、子进程成了孤儿的时候。
- **占用者是别的程序** → 不再让你看 `exit status 2`，横幅直接给出可执行的信息：

  > 本地端口 34455 已被 powershell.exe（PID 12540）占用：结束它，或在「连接」里换一个端口

客户端因其他原因退出时，横幅也会带上它最后打印的那行错误，而不是只报退出码。

### 隧道起来了、RTT 也有，但打不开网页

先按根因那条查（服务器内核 echo 与 pingtunnel 回包竞争，见下一节），再按「某站打不开」的五条排查。

## 自动化构建（GitHub Actions）

`.github/workflows/build-windows.yml`：

- **平时**：任意 push / PR 都跑一次，产物作为 workflow artifact（zip）上传。
- **打 tag**（`v*`）：同一个 zip 自动附加到 GitHub Release。
- **手动**：Actions 里点 Run workflow，可填 `client_tag` 指定客户端版本，留空则取 latest。

关键一步是“把客户端一起装进包里”：`scripts/fetch-client.ps1` 会去 `esrrhs/pingtunnel` 的 release 里找 `pingtunnel_windows_amd64.zip`，解开、取出 `pingtunnel.exe`、写到应用旁边，并记录版本号到 `client-version.txt`；然后连同 `wintun.dll`、`使用说明.txt` 一起压成：

```
pingtunnel-workbench-<tag>-windows-amd64.zip
  ├─ pingtunnel-workbench.exe
  ├─ pingtunnel.exe            ← 自动从上游 release 下载
  ├─ client-version.txt        ← 实际使用的客户端版本
  ├─ wintun.dll / wintun-LICENSE.txt
  ├─ 使用说明.txt
  └─ pingtunnel-workbench Setup 2.1.0 amd64.exe
```

仓库不存 16 MB 的客户端二进制（`.gitignore` 已排除），本地 `build.bat` 第一次跑发现缺 `bin\pingtunnel.exe` 时也会自动下载。要固定版本：

```bat
powershell -File scripts\fetch-client.ps1 -Tag 2.4
```

> 版本要对得上服务端：上游 latest 是 2026 重构版，若你的服务端是旧版（如 `jiqing112` 分叉），就用 `-Tag 2.4` 这种当年的版本，或直接用你自己的 `pingtunnel.exe` 覆盖。

## 某站（如 Twitter）打不开？

先分清是“隧道不通”还是“这个站的事”。一条命令就够：

```bat
curl --socks5-hostname 127.0.0.1:34455 -m 15 https://www.baidu.com
curl --socks5-hostname 127.0.0.1:34455 -m 20 https://x.com
```

- **两个都超时** ⇒ 不是站点问题，是隧道没通（先看本文“实测记录”与 `docs/诊断记录.md`）。
- **百度通、X 不通** ⇒ 才轮到下面五条。

1. **DNS**：用 `--socks5-hostname`（域名交给代理解析）对比 `--socks5`（本地解析）。后者拿到污染 IP 就必死。浏览器走 socks5 时默认会把域名交给代理解析，但开了「安全 DNS / DoH」就可能绕过。
2. **QUIC / HTTP3**：Chrome 会先试 UDP 443，pingtunnel 的 socks5 未必支持 UDP ASSOCIATE，失败后才回落 TCP——体感就是“卡很久才出来”或直接报错。可先用 `chrome://flags` 关掉 QUIC 对照，或在浏览器里对 x.com 禁用 QUIC。
3. **IPv6**：x.com 有 AAAA。隧道只承载 IPv4 时，优先 V6 会直接失败；`curl -4` 对比 `curl -6` 即可确认。
4. **MTU / 分片**：TLS 握手包（证书链）比较大，ICMP 隧道封装开销也大。TUN 默认 1400 就是为此；如果握手总在第一次大包时断，往下调到 1280 试。
5. **并发与带宽**：X 首页会同时拉 `abs.twimg.com`、`pbs.twimg.com`、`api.x.com` 等十几条连接，全挤在一条 ICMP 通道上（`tcp_mw` 窗口就那么大）。RTT 140ms 下，一条连接的吞吐上限 = 窗口/RTT，粗算几十 KB/s —— 轻量站能开，X 这种会直接超时。

## 全局代理（系统代理）

Windows 的“系统代理”只认 HTTP 代理，不认 SOCKS。所以开启 socks5 模式后，程序会在 `127.0.0.1:34457` 起一个本地 HTTP→SOCKS5 桥（CONNECT 与普通 HTTP 都走隧道），再把系统代理指过去。

- 开关在右侧「出口」面板；**默认关闭**。
- 安全阀：隧道还没收到过 pong 时开关是禁用的 —— 否则整台机器会指向一个不通的代理，浏览器直接打不开网页。
- 停止隧道 / 退出程序会自动还原系统代理（开之前的值会被记住并恢复，不会吃掉你原来的公司代理配置）。
- 覆盖范围：走 WinINET 的程序（Edge、Chrome、Office、大部分安装器）。**不认系统代理的程序（很多命令行工具、部分 Electron 应用、游戏）不受影响** —— 那需要 TUN 级方案，见下。

要真正的“全局”（所有程序，含不认代理的），得用虚拟网卡（wintun）+ 路由与 DNS 接管，把 IP 包转进隧道，还要处理 ICMP 隧道下的 MTU 与分片。那是另一个量级的工程（需要装驱动、常驻管理员），当前版本没做。

## 实测记录与结论（已定案）

本次实机配置：服务器 `<SERVER_IP>`，本地端口 `34455`，密钥 `<KEY>`，TCP + socks5；实测 RTT `136~141ms`。

**根因：不是版本问题，也不是服务端故障。** 同一个 ICMP 请求会引出两个 `(id, seq)` 相同的回包——服务器内核的自动 echo（`rproto=0`，原样字节）和 pingtunnel 服务端的回复（`rproto=-1`，改过一个字节）。这台客户端是 VMware 虚拟机，其 NAT 的 ICMP 处理对同一请求只放行一个回包，内核那条先到，pingtunnel 那条被判重复丢掉。于是"别的电脑能连、这台不行"。

**修复**（已落在服务器上）：关掉内核 echo，让每请求只剩一个回包。

```bash
# /etc/sysctl.d/99-pingtunnel.conf
net.ipv4.icmp_echo_ignore_all = 1
```

代价是服务器不再响应 ping；如果依赖 ping 做存活监控，改用 TCP 探测，或者改用另一条路——**把客户端虚拟机网络从 NAT 改成桥接**，不改服务器也能绕开。

验证：`curl --socks5-hostname 127.0.0.1:34455 https://www.baidu.com` → `code=200`，界面 RTT 136ms、每秒一个 pong。

完整证据链、实验命令、以及"为什么前面几轮判断错了"的复盘，见 [docs/诊断记录.md](docs/诊断记录.md)。

> 顺带发现：服务端在这条老分支上有 `processDataPacket` 空指针（`server.go:266`），22:11 与 22:39 各崩一次，靠 systemd `Restart=always` 拉起。修好回包后本轮未复现；若频繁出现，把服务端换成上游新版即可（协议兼容）。

## 开发

```powershell
. .\dev.ps1                     # 本仓库把 Go 1.27.3 放在 .toolchain\，并设 goproxy.cn
cd pingtunnel-workbench
bun run dev                     # mygo dev：Vite 热更新 + 应用窗口
bun run build                   # 打包到 build\windows-amd64\
```

```powershell
# 驱动同一套服务做端到端自检（需要真实服务器）
$env:PT_SERVER='host'; $env:PT_KEY='n'
go test -tags smoke -run TestAgainstRealServer -v .
```

结构：

```
pingtunnel-workbench/
├── main.go            窗口、托盘、事件绑定
├── service.go         会话状态机：进程启停、日志/统计解析、ICMP 探测
├── client.go          定位 pingtunnel.exe、探测参数、拼命令行、写 session.json
├── settings.go        设置读写与校验（json，带 BOM 容错）
├── logsink.go         会话日志环形缓冲、级别过滤、降噪
├── proc_windows.go    CREATE_NO_WINDOW，避免闪控制台
├── index.html         单页工作台
└── src/style.css      明暗双主题的设计 token 与组件样式
```

## 已知限制

- Windows 上 ICMP 需要管理员权限，非管理员时界面会直接给提示；
- 托盘菜单的「启动/停止」用的是已保存的设置，不会读取窗口里未保存的改动（改动是即时保存的，所以两者一致）；
- 隧道客户端的参数集合来自 `-h` 探测，个别非常古老的构建若 `-h` 无输出，会退回一套保守的通用参数。
