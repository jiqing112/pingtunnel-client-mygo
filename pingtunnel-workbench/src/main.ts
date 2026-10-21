import { Tunnel, events, type LogLine, type Settings, type Status } from "./mygo";
import { isMyGo } from "mygo-runtime";

/* ------------------------------------------------------------- elements */

const $ = <T extends HTMLElement = HTMLElement>(selector: string): T => {
  const el = document.querySelector<T>(selector);
  if (!el) throw new Error(`missing element: ${selector}`);
  return el;
};

const app = $("#app");
const banner = $("#banner");
const bannerText = $("#banner-text");
const form = $<HTMLFormElement>("#form");
const summary = $("#summary");
const summaryList = $("#summary-list");
const summaryTags = $("#summary-tags");
const hint = $("#hint");
const startBtn = $<HTMLButtonElement>("#start-btn");
const probeBtn = $<HTMLButtonElement>("#probe-btn");
const probeLabel = $("#probe-label");
const quitBtn = $<HTMLButtonElement>("#quit-btn");
const stateText = $("#state-text");
const heroMeta = $("#hero-meta");
const rttValue = $("#rtt-value");
const rttLabel = $("#rtt-label");
const diagnose = $("#diagnose");
const diagCopy = $("#diag-copy");
const diagKey = $("#diag-key");
const sparkHint = $("#spark-empty");
const sparkLine = $("#spark-line");
const sparkArea = $("#spark-area");
const sparkEmpty = $("#spark-empty");
const sparkBox = $("#spark");
const mUp = $("#m-up");
const mDown = $("#m-down");
const mConn = $("#m-conn");
const mUptime = $("#m-uptime");
const logBody = $("#log-body");
const logList = $("#log-list");
const logEmpty = $("#log-empty");
const logCount = $("#log-count");
const toasts = $("#toasts");
const themeBtn = $<HTMLButtonElement>("#theme-btn");
const themeIcon = $("#theme-icon");
const mainPane = $("#main-pane");

let settings: Settings = fallbackSettings();
let status: Status | null = null;
let busy: "" | "start" | "stop" | "probe" = "";
let diagOpen = false; // the card is closed until the user asks for it

function fallbackSettings(): Settings {
  return {
    server: "",
    listenPort: "4455",
    target: "",
    timeout: "60",
    key: "0",
    tcpMode: true,
    tcpBuffer: "1048576",
    tcpMaxWin: "10000",
    tcpResend: "400",
    tcpCompress: "0",
    tcpStat: false,
    socks5: false,
    socks5User: "",
    socks5Pass: "",
    maxConn: "1000",
    congestion: "bb",
    noLog: false,
    logLevel: "info",
    icmpAddr: "",
    encrypt: "",
    encryptKey: "",
    binaryPath: "",
    s5filter: "",
    s5ftfile: "",
    systemProxy: false,
    httpProxyPort: "34457",
    tun: false,
    tunMtu: "1400",
  };
}

/* ---------------------------------------------------------------- theme */

type ThemeMode = "auto" | "light" | "dark";
const THEME_KEY = "pingtunnel.theme";

const sunIcon =
  '<circle cx="8" cy="8" r="3.1" stroke="currentColor" stroke-width="1.3"/><path d="M8 1.2v1.4M8 13.4v1.4M14.8 8h-1.4M2.6 8H1.2M12.8 3.2l-1 1M4.2 11.8l-1 1M12.8 12.8l-1-1M4.2 4.2l-1-1" stroke="currentColor" stroke-width="1.3" stroke-linecap="round"/>';
const moonIcon =
  '<path d="M13.4 9.6A5.8 5.8 0 0 1 6.4 2.6a5.9 5.9 0 1 0 7 7Z" stroke="currentColor" stroke-width="1.3" stroke-linejoin="round"/>';
const autoIcon =
  '<circle cx="8" cy="8" r="6.3" stroke="currentColor" stroke-width="1.3"/><path d="M8 1.7v12.6a6.3 6.3 0 0 0 0-12.6Z" fill="currentColor"/>';

function applyTheme(mode: ThemeMode): void {
  if (mode === "auto") document.documentElement.removeAttribute("data-theme");
  else document.documentElement.dataset.theme = mode;
  themeIcon.innerHTML = mode === "auto" ? autoIcon : mode === "light" ? sunIcon : moonIcon;
  themeBtn.title = `外观：${mode === "auto" ? "跟随系统" : mode === "light" ? "浅色" : "深色"}（点击切换）`;
  themeBtn.setAttribute("aria-label", themeBtn.title);
  try {
    localStorage.setItem(THEME_KEY, mode);
  } catch {
    /* a locked-down webview may refuse storage */
  }
}

function currentTheme(): ThemeMode {
  try {
    const saved = localStorage.getItem(THEME_KEY);
    if (saved === "light" || saved === "dark" || saved === "auto") return saved;
  } catch {
    /* ignore */
  }
  return "auto";
}

themeBtn.addEventListener("click", () => {
  const order: ThemeMode[] = ["auto", "light", "dark"];
  const next = order[(order.indexOf(currentTheme()) + 1) % order.length] ?? "auto";
  applyTheme(next);
  toast(`外观已切换为${next === "auto" ? "跟随系统" : next === "light" ? "浅色" : "深色"}`);
});

applyTheme(currentTheme());

/* ---------------------------------------------------------------- toast */

function toast(message: string, tone: "ok" | "err" | "info" = "info"): void {
  const el = document.createElement("div");
  el.className = "toast";
  el.dataset.tone = tone;
  el.textContent = message;
  toasts.appendChild(el);
  setTimeout(() => {
    el.dataset.leaving = "true";
    setTimeout(() => el.remove(), 200);
  }, 2600);
}

function showBanner(message: string, tone: "error" | "warn" = "error"): void {
  bannerText.textContent = message;
  banner.classList.toggle("banner--warn", tone === "warn");
  banner.hidden = false;
}

function hideBanner(): void {
  banner.hidden = true;
}

$("#banner-close").addEventListener("click", hideBanner);

// The diagnose card is a floating aside: it can be put away, and reopened from
// the line that summarises it in the hero.
$("#diag-close").addEventListener("click", () => {
  diagOpen = false;
  diagnose.hidden = true;
});

// The summary line in the hero is the only place the card is announced: a
// silent tunnel should not throw a panel over the page by itself.
rttLabel.addEventListener("click", () => {
  if (rttLabel.dataset.action !== "true") return;
  diagOpen = !diagOpen;
  diagnose.hidden = !diagOpen;
});

$("#tun").addEventListener("change", async (event) => {
  const input = event.target as HTMLInputElement;
  const on = input.checked;
  if (busy) {
    input.checked = !on;
    return;
  }
  if (on) toast("正在验证数据通路，可能要几秒…");
  try {
    if (!isMyGo()) throw new Error("请通过 mygo 启动应用后使用");
    renderStatus(await Tunnel.setTun(on));
    toast(on ? "TUN 已接管全机流量" : "TUN 已关闭，路由已恢复", on ? "ok" : "info");
  } catch (err) {
    input.checked = !on;
    showBanner(err instanceof Error ? err.message : String(err), "warn");
  }
});

$("#systemProxy").addEventListener("change", async (event) => {
  const input = event.target as HTMLInputElement;
  const on = input.checked;
  if (busy) {
    input.checked = !on;
    return;
  }
  try {
    if (!isMyGo()) throw new Error("请通过 mygo 启动应用后使用");
    renderStatus(await Tunnel.setSystemProxy(on));
    toast(on ? "系统代理已指向本机隧道" : "系统代理已还原", on ? "ok" : "info");
  } catch (err) {
    input.checked = !on;
    showBanner(err instanceof Error ? err.message : String(err), "warn");
  }
});

/* ------------------------------------------------------------ accordion */

for (const group of Array.from(document.querySelectorAll<HTMLElement>(".group"))) {
  const head = group.querySelector<HTMLButtonElement>(".group__head");
  head?.addEventListener("click", () => {
    const open = group.dataset.open !== "false";
    group.dataset.open = open ? "false" : "true";
    head.setAttribute("aria-expanded", String(!open));
  });
}

function openGroup(id: string): void {
  const group = document.getElementById(id);
  if (!group) return;
  group.dataset.open = "true";
  group.querySelector(".group__head")?.setAttribute("aria-expanded", "true");
}

/* ------------------------------------------------------------ form glue */

const byId = (id: string): string => ($<HTMLInputElement>(`#${id}`).value || "").trim();
const box = (id: string): boolean => $<HTMLInputElement>(`#${id}`).checked;
const setValue = (id: string, value: string): void => {
  $<HTMLInputElement>(`#${id}`).value = value;
};
const setChecked = (id: string, value: boolean): void => {
  $<HTMLInputElement>(`#${id}`).checked = value;
};

function readForm(): Settings {
  return {
    server: byId("server"),
    listenPort: byId("listenPort"),
    target: byId("target"),
    timeout: byId("timeout"),
    key: byId("key"),
    tcpMode: box("tcpMode"),
    tcpBuffer: byId("tcpBuffer"),
    tcpMaxWin: byId("tcpMaxWin"),
    tcpResend: byId("tcpResend"),
    tcpCompress: byId("tcpCompress"),
    tcpStat: box("tcpStat"),
    socks5: box("socks5"),
    socks5User: byId("socks5User"),
    socks5Pass: byId("socks5Pass"),
    maxConn: byId("maxConn"),
    congestion: byId("congestion"),
    noLog: box("noLog"),
    logLevel: byId("logLevel"),
    icmpAddr: byId("icmpAddr"),
    encrypt: byId("encrypt"),
    encryptKey: byId("encryptKey"),
    binaryPath: byId("binaryPath"),
    s5filter: byId("s5filter"),
    s5ftfile: byId("s5ftfile"),
    systemProxy: box("systemProxy"),
    httpProxyPort: byId("httpProxyPort"),
    tun: box("tun"),
    tunMtu: byId("tunMtu"),
  };
}

function writeForm(s: Settings): void {
  setValue("server", s.server);
  setValue("listenPort", s.listenPort);
  setValue("target", s.target);
  setValue("timeout", s.timeout);
  setValue("key", s.key);
  setChecked("tcpMode", s.tcpMode);
  setValue("tcpBuffer", s.tcpBuffer);
  setValue("tcpMaxWin", s.tcpMaxWin);
  setValue("tcpResend", s.tcpResend);
  setValue("tcpCompress", s.tcpCompress);
  setChecked("tcpStat", s.tcpStat);
  setChecked("socks5", s.socks5);
  setValue("socks5User", s.socks5User);
  setValue("socks5Pass", s.socks5Pass);
  setValue("maxConn", s.maxConn);
  setValue("congestion", s.congestion || "bb");
  setChecked("noLog", s.noLog);
  setValue("logLevel", s.logLevel || "info");
  setValue("icmpAddr", s.icmpAddr);
  setValue("encrypt", s.encrypt);
  setValue("encryptKey", s.encryptKey);
  setValue("binaryPath", s.binaryPath);
  setValue("s5filter", s.s5filter);
  setValue("s5ftfile", s.s5ftfile);
  setValue("httpProxyPort", s.httpProxyPort);
  $<HTMLInputElement>("#systemProxy").checked = s.systemProxy;
  setValue("tunMtu", s.tunMtu);
  $<HTMLInputElement>("#tun").checked = s.tun;
  syncConditionalFields();
}

/** Fields that only make sense in one mode grey out instead of vanishing, so
 * the form keeps its shape while the user flips switches. */
function syncConditionalFields(): void {
  const socks5 = box("socks5");
  const target = $<HTMLInputElement>("#target");
  target.disabled = socks5;
  target.placeholder = socks5 ? "socks5 模式下不需要" : "127.0.0.1:8080";
  $("#socks5-text").textContent = socks5 ? "客户端充当本地 socks5 代理" : "关闭时按端口转发";

  const s5user = $<HTMLInputElement>("#socks5User");
  const s5pass = $<HTMLInputElement>("#socks5Pass");
  s5user.disabled = !socks5;
  s5pass.disabled = !socks5;

  const tcp = box("tcpMode");
  $("#tcp-text").textContent = tcp ? "TCP 模式已启用" : "UDP 模式（原始 ICMP 负载）";
  for (const id of ["tcpBuffer", "tcpMaxWin", "tcpResend", "tcpCompress", "tcpStat"]) {
    $<HTMLInputElement>(`#${id}`).disabled = !tcp;
  }
  if (!tcp) openGroup("group-tcp");

  const encrypt = byId("encrypt");
  const encKey = $<HTMLInputElement>("#encryptKey");
  encKey.disabled = encrypt === "";
  encKey.placeholder = encrypt === "" ? "先选择加密方式" : "服务端需一致";
  if (encrypt !== "" && encKey.value === "") openGroup("group-advanced");

  const path = $<HTMLInputElement>("#binaryPath");
  path.title = path.value || "自动查找：程序目录 → 上级目录 → PATH";
}

$("#binary-btn").addEventListener("click", async () => {
  if (!isMyGo()) return;
  try {
    const chosen = await Tunnel.chooseBinary();
    if (chosen) {
      $<HTMLInputElement>("#binaryPath").value = chosen;
      settings.binaryPath = chosen;
      toast(`已选择客户端：${chosen}`, "ok");
      void refreshInfo();
    }
  } catch (err) {
    showBanner(err instanceof Error ? err.message : String(err));
  }
});

form.addEventListener("input", () => {
  syncConditionalFields();
  scheduleSave();
});
form.addEventListener("change", () => {
  syncConditionalFields();
  scheduleSave();
});
form.addEventListener("submit", (e) => e.preventDefault());

let saveTimer = 0;
function scheduleSave(): void {
  window.clearTimeout(saveTimer);
  saveTimer = window.setTimeout(async () => {
    if (!isMyGo()) return;
    try {
      settings = await Tunnel.saveSettings(readForm());
    } catch {
      /* saving is best effort: a bad field must not nag while typing */
    }
  }, 500);
}

/* ---------------------------------------------------------------- status */

const stateLabel: Record<string, string> = {
  idle: "未连接",
  starting: "连接中",
  running: "运行中",
  stopping: "停止中",
  error: "出错了",
};

function renderStatus(next: Status): void {
  const prev = status;
  status = next;
  app.dataset.state = next.state;
  stateText.textContent = stateLabel[next.state] ?? next.state;

  const mode = next.mode === "udp" ? "UDP" : "TCP";
  const role = next.role === "socks5" ? "socks5 代理" : "端口转发";
  const host = next.serverIp || next.server;
  const active = next.state !== "idle";
  heroMeta.textContent = active && host ? `${host} · ${mode} · ${role}` : "没有正在运行的隧道";

  rttValue.textContent = next.rttMs > 0 ? String(next.rttMs) : "—";
  rttValue.dataset.empty = String(!(next.rttMs > 0));
  const pongAge = next.pongAge ?? 0;
  rttLabel.textContent =
    next.rttMs > 0
      ? "往返延迟"
      : pongAge >= 15
        ? `已 ${pongAge}s 没有回包 · 排查`
        : pongAge >= 10
          ? `已 ${pongAge}s 没有回包`
          : "等待服务器回包";
  sparkHint.textContent =
    next.rttMs > 0
      ? ""
      : pongAge >= 10
        ? "还没有 pong，见下方排查"
        : "开始后这里显示延迟曲线";

  // The tunnel can be up while the server stays silent: say so instead of
  // showing a healthy green dashboard with no numbers behind it.
  const stuck = next.state === "running" && pongAge >= 15;
  if (!stuck) diagOpen = false; // a pong, or the end of the session, closes it
  diagnose.hidden = !stuck || !diagOpen;
  if (stuck) {
    const everPonged = (next.history?.length ?? 0) > 0;
    diagCopy.innerHTML = everPonged
      ? `已经 <b>${pongAge}</b> 秒没有收到 pong（上一次 ${next.rttMs}ms）。按顺序检查：`
      : `已经等了 <b>${pongAge}</b> 秒，一个 pong 都没收到。按顺序检查：`;
    diagKey.textContent = String(next.key ?? 0);
  }
  // One quiet line, clickable: that is all a stuck session shows by default.
  if (stuck) {
    rttLabel.dataset.action = "true";
    rttLabel.title = diagOpen ? "收起排查清单" : "查看排查清单";
  } else {
    rttLabel.removeAttribute("data-action");
    rttLabel.removeAttribute("title");
  }

  mUp.textContent = String(next.sendKBs ?? 0);
  mDown.textContent = String(next.recvKBs ?? 0);
  mConn.textContent = String(next.conn ?? 0);
  mUptime.textContent = formatUptime(next.uptimeSec ?? 0);

  renderSpark(next.history ?? []);

  const running = next.state === "running";
  app.dataset.running = String(running);
  summary.hidden = !running;
  form.hidden = running;
  if (running) renderSummary(next);
  else if (prev?.state === "running" && next.state === "idle") writeForm(settings);

  const live = next.state === "running" || next.state === "stopping";
  startBtn.textContent = live ? "停止隧道" : "启动隧道";
  startBtn.classList.toggle("btn--primary", !live);
  startBtn.classList.toggle("btn--danger", live);
  startBtn.disabled = busy !== "" || next.state === "starting" || next.state === "stopping";

  // Both switches live outside the form, so they stay reachable while a session
  // runs. They are usable only while running; opening one probes the tunnel
  // first and reports why it refused, instead of sitting there greyed out.
  const sysProxy = $<HTMLInputElement>("#systemProxy");
  if (sysProxy.checked !== next.systemProxy) sysProxy.checked = next.systemProxy;
  sysProxy.disabled = !running;
  $("#sysproxy-addr").textContent = running
    ? `http://${next.proxyAddr || "…"}`
    : "HTTP · 只在运行后可用";

  const socksMode = running ? next.role === "socks5" : settings.socks5;
  const tun = $<HTMLInputElement>("#tun");
  if (tun.checked !== next.tun) tun.checked = next.tun;
  tun.disabled = !running || !socksMode;
  $("#tun-addr").textContent = socksMode
    ? `wintun · MTU ${settings.tunMtu || "1400"}`
    : "wintun · 需要 socks5 模式";

  $("#outbound-note").textContent = !running
    ? "未运行"
    : next.systemProxy || next.tun
      ? "已接管"
      : next.proxyReady
        ? "可打开"
        : "隧道未响应";
  $("#outbound-hint").textContent = !running
    ? "启动隧道后可用；打开前会先验证隧道能否承载数据"
    : next.systemProxy || next.tun
      ? "停止隧道或退出程序会自动还原；TUN 在 20 秒无回包时自动关闭"
      : next.proxyReady
        ? "点开时会先做一次 socks5 连通测试，没通过会告诉你原因"
        : "隧道还没回包；现在打开会先测数据通路，失败就不接管";

  // Only clear the banner when the session actually changes state. The status
  // arrives once a second, and hiding on every tick would swallow the message
  // a refused switch just put up.
  if (next.state === "error" && next.message) showBanner(next.message);
  else if (prev && prev.state !== next.state) hideBanner();

  renderHint();
}

function renderHint(): void {
  if (busy === "probe") {
    hint.textContent = "正在测试服务器可达性…";
    return;
  }
  if (!status) {
    hint.textContent = "填写服务器与转发目标后启动";
    return;
  }
  if (status.state === "running") {
    const up = formatBytes(status.sendTotalKB ?? 0);
    const down = formatBytes(status.recvTotalKB ?? 0);
    hint.innerHTML = `本地 <b>127.0.0.1:${escapeHtml(status.listen || settings.listenPort)}</b> → <b>${escapeHtml(status.server)}</b> ｜ 累计 上行 ${up} · 下行 ${down}`;
    return;
  }
  if (status.state === "error") {
    hint.textContent = "启动失败，修正后重试";
    return;
  }
  if (status.state === "starting") {
    hint.textContent = "正在建立 ICMP 隧道…";
    return;
  }
  if (status.state === "stopping") {
    hint.textContent = "正在停止…";
    return;
  }
  hint.textContent = "填写服务器与转发目标后启动";
}

function renderSummary(next: Status): void {
  const rows: [string, string][] = [
    ["服务器", next.server || settings.server],
    ["本地端口", `127.0.0.1:${next.listen || settings.listenPort}`],
    next.role === "socks5" ? ["代理模式", "socks5"] : ["转发目标", next.target || settings.target],
    ["密钥", next.key ? String(next.key) : "0（无需）"],
    ["模式", next.mode.toUpperCase()],
    ["最大连接", settings.maxConn],
  ];
  summaryList.innerHTML = rows
    .map(
      ([k, v]) => `<div><dt>${k}</dt><dd title="${escapeHtml(v)}">${escapeHtml(v)}</dd></div>`,
    )
    .join("");

  const tags: string[] = [];
  if (settings.tcpMode) {
    tags.push(`bs ${settings.tcpBuffer}`, `mw ${settings.tcpMaxWin}`, `rst ${settings.tcpResend}ms`);
    if (settings.tcpCompress !== "0") tags.push(`gz ${settings.tcpCompress}`);
    if (settings.tcpStat) tags.push("stat");
  } else {
    tags.push("udp 模式");
  }
  tags.push(`congestion ${settings.congestion || "off"}`);
  if (settings.encrypt) tags.push(`encrypt ${settings.encrypt}`);
  if (settings.noLog) tags.push("nolog");
  summaryTags.innerHTML = tags.map((t) => `<span class="tag">${escapeHtml(t)}</span>`).join("");
}

function formatUptime(total: number): string {
  const s = Math.max(0, Math.floor(total));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  const pad = (n: number) => String(n).padStart(2, "0");
  return h > 0 ? `${pad(h)}:${pad(m)}:${pad(sec)}` : `${pad(m)}:${pad(sec)}`;
}

function formatBytes(kb: number): string {
  if (kb >= 1024 * 1024) return `${(kb / 1024 / 1024).toFixed(2)} GB`;
  if (kb >= 1024) return `${(kb / 1024).toFixed(1)} MB`;
  return `${kb} KB`;
}

function escapeHtml(value: string | number): string {
  const entities: Record<string, string> = {
    "&": "&amp;",
    "<": "&lt;",
    ">": "&gt;",
    '"': "&quot;",
    "'": "&#39;",
  };
  return String(value).replace(/[&<>"']/g, (c) => entities[c] ?? c);
}

/* ------------------------------------------------------------- sparkline */

const W = 320;
const H = 62;
const PAD = 4;

function renderSpark(values: number[]): void {
  const samples = values.filter((v) => v > 0);
  if (samples.length === 0) {
    sparkLine.setAttribute("d", "");
    sparkArea.setAttribute("d", "");
    sparkBox.dataset.empty = "true";
    return;
  }
  sparkBox.dataset.empty = "false";

  // The newest sample sits at the right edge and older ones grow leftwards, so
  // a young session looks like a recording, not a stretched flat line.
  const SLOTS = 90;
  const step = W / (SLOTS - 1);
  const max = Math.max(40, ...samples) * 1.15;
  const y = (v: number) => H - PAD - (Math.min(v, max) / max) * (H - PAD * 2);

  const points: [number, number][] = samples.map((v, i) => [
    W - (samples.length - 1 - i) * step,
    y(v),
  ]);
  if (points.length === 1) points.unshift([W - 1.5, points[0]?.[1] ?? 0]);

  const segments: [number, number][][] = points.length ? [points] : [];

  const line = segments
    .map((s) =>
      s.map(([x, yy], i) => `${i === 0 ? "M" : "L"}${x.toFixed(1)},${yy.toFixed(1)}`).join(" "),
    )
    .join(" ");


  const area = segments
    .filter((s) => s.length > 12)
    .map((s) => {
      const first = s[0];
      const last = s[s.length - 1];
      if (!first || !last) return "";
      const head = `M${first[0].toFixed(1)},${H}`;
      const body = s.map(([x, yy]) => `L${x.toFixed(1)},${yy.toFixed(1)}`).join(" ");
      const tail = `L${last[0].toFixed(1)},${H}Z`;
      return head + body + tail;
    })
    .join(" ");

  sparkLine.setAttribute("d", line);
  sparkArea.setAttribute("d", area);
}

/* ------------------------------------------------------------------ logs */

const MAX_LINES = 600;
let logTotal = 0;

function appendLogs(lines: LogLine[], reset = false): void {
  if (reset) clearLogDom();
  if (lines.length === 0) return;

  const nearBottom = logBody.scrollHeight - logBody.scrollTop - logBody.clientHeight < 48;
  const frag = document.createDocumentFragment();
  for (const line of lines) {
    const row = document.createElement("div");
    row.className = "log-line";
    row.dataset.level = line.level;
    const time = document.createElement("span");
    time.className = "log-line__time";
    time.textContent = line.time;
    const text = document.createElement("span");
    text.className = "log-line__text";
    text.textContent = line.text;
    row.append(time, text);
    frag.appendChild(row);
    logTotal++;
  }
  logList.appendChild(frag);

  while (logList.childElementCount > MAX_LINES) logList.firstElementChild?.remove();
  logEmpty.hidden = logList.childElementCount > 0;
  logCount.textContent = String(logTotal);
  if (nearBottom) logBody.scrollTop = logBody.scrollHeight;
}

function clearLogDom(): void {
  logList.replaceChildren();
  logTotal = 0;
  logCount.textContent = "0";
  logEmpty.hidden = false;
}

$("#log-clear").addEventListener("click", async () => {
  clearLogDom();
  if (isMyGo()) await Tunnel.clearLogs().catch(() => undefined);
});

/* --------------------------------------------------------------- actions */

function paintStartButton(): void {
  const live = status?.state === "running" || status?.state === "stopping";
  startBtn.textContent = live ? "停止隧道" : "启动隧道";
  startBtn.classList.toggle("btn--primary", !live);
  startBtn.classList.toggle("btn--danger", live);
  startBtn.disabled = busy !== "" || status?.state === "starting" || status?.state === "stopping";
  startBtn.toggleAttribute("data-busy", busy === "start" || busy === "stop");
}

async function toggle(): Promise<void> {
  if (busy) return;
  const live = status?.state === "running" || status?.state === "stopping";
  busy = live ? "stop" : "start";
  startBtn.disabled = true;
  startBtn.innerHTML = `<span class="spinner"></span>${live ? "正在停止…" : "正在启动…"}`;
  hideBanner();
  renderHint();
  try {
    if (!isMyGo()) throw new Error("请通过 mygo 启动应用后使用");
    if (live) {
      renderStatus(await Tunnel.stop());
      toast("隧道已停止");
    } else {
      settings = readForm();
      renderStatus(await Tunnel.start(settings));
      toast("隧道已启动，等待首个回包", "ok");
      if (settings.noLog) toast("已按设置关闭会话日志");
    }
  } catch (err) {
    showBanner(err instanceof Error ? err.message : String(err));
  } finally {
    busy = "";
    startBtn.innerHTML = "";
    paintStartButton();
    renderHint();
  }
}

startBtn.addEventListener("click", toggle);

// Ctrl/Cmd+Enter starts or stops: the one action worth a shortcut.
window.addEventListener("keydown", (e) => {
  if ((e.ctrlKey || e.metaKey) && e.key === "Enter") {
    e.preventDefault();
    void toggle();
  }
  if (e.key === "Escape") hideBanner();
});

probeBtn.addEventListener("click", async () => {
  if (busy) return;
  const host = byId("server");
  if (!host) {
    showBanner("请先填写服务器地址", "warn");
    $<HTMLInputElement>("#server").focus();
    return;
  }
  busy = "probe";
  probeBtn.disabled = true;
  probeLabel.textContent = "测试中…";
  renderHint();
  try {
    if (!isMyGo()) throw new Error("请通过 mygo 启动应用后使用");
    const result = await Tunnel.probe(host);
    if (result.alive) {
      toast(`${result.ip} 通 ${result.recv}/${result.sent} · 平均 ${result.avgMs} ms`, "ok");
      hint.textContent = `${result.ip} 可达 · ${result.minMs}-${result.maxMs} ms`;
    } else {
      showBanner(result.message || "没有收到 ICMP 回包", "warn");
    }
  } catch (err) {
    showBanner(err instanceof Error ? err.message : String(err), "warn");
  } finally {
    busy = "";
    probeBtn.disabled = false;
    probeLabel.textContent = "测试服务器";
    renderHint();
  }
});

quitBtn.addEventListener("click", async () => {
  if (!isMyGo()) return;
  await Tunnel.quit().catch(() => undefined);
});

/* ------------------------------------------------------------- scroll cue */

mainPane.addEventListener("scroll", () => {
  app.dataset.scrolled = String(mainPane.scrollTop > 2);
});

/* ---------------------------------------------------------------- startup */

events.status.on((next) => renderStatus(next));

events.logs.on((batch) => {
  appendLogs(batch.lines ?? [], Boolean(batch.reset));
});

async function refreshInfo(): Promise<void> {
  try {
    const info = await Tunnel.info();
    const field = $<HTMLInputElement>("#binaryPath");
    const resolved = info.clientPath;
    if (!field.value) field.placeholder = resolved || "未找到 pingtunnel.exe";
    field.title = resolved || "自动查找：程序目录 → 上级目录 → PATH";
    if (!resolved) {
      showBanner("没有找到 pingtunnel.exe：把它放到本程序旁边，或展开「日志与高级」点「选择…」指定路径。", "warn");
    } else if (!info.admin) {
      showBanner("当前进程没有 ICMP 原始套接字权限，启动多半会失败：请以管理员身份运行。", "warn");
    }
  } catch {
    /* the first window paints before Go is reachable; boot retries */
  }
}

async function boot(): Promise<void> {
  if (!isMyGo()) {
    // mygo-runtime is injected by the window, so a plain browser has none of
    // it: the page still lays out, it just cannot talk to Go.
    writeForm(settings);
    startBtn.disabled = true;
    probeBtn.disabled = true;
    hint.textContent = "请用 mygo dev 或打包后的 pingtunnel 启动";
    return;
  }

  try {
    const [saved, current, lines] = await Promise.all([
      Tunnel.settings(),
      Tunnel.status(),
      Tunnel.logs(),
    ]);
    settings = saved;
    writeForm(saved);
    renderStatus(current);
    appendLogs(lines);
    await refreshInfo();
  } catch (err) {
    showBanner(err instanceof Error ? err.message : String(err));
  }
}

void boot();

// Keep the numbers honest when the window comes back from the tray.
window.addEventListener("focus", () => {
  if (!isMyGo()) return;
  void Tunnel.status()
    .then(renderStatus)
    .catch(() => undefined);
});

