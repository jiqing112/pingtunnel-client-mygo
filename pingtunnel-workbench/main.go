package main

import (
	_ "embed"
	"log"
	"runtime"

	"github.com/egoist/mygo"
	"golang.org/x/net/icmp"
)

const appVersion = "2.1.0"

//go:embed resources/tray.png
var trayIcon []byte

func platformName() string {
	switch runtime.GOOS {
	case "windows":
		return "Windows"
	case "darwin":
		return "macOS"
	default:
		return "Linux"
	}
}

// canRawICMP reports whether this process may open the raw ICMP socket the
// probe (and the client's own tunnel) needs, which is what "run as
// administrator" buys on Windows.
func canRawICMP() bool {
	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// shortPath keeps a long Windows path readable in the narrow log panel: the
// tail says where the file is, the ellipsis says it started further left.
func shortPath(p string) string {
	const max = 46
	runes := []rune(p)
	if len(runes) <= max {
		return p
	}
	return "…" + string(runes[len(runes)-max+1:])
}

func main() {
	sink := newLogSink()
	store := NewStore()
	tunnel := NewTunnel(store, sink)
	mygo.Bind(tunnel)

	quitting := false
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { quitting = true })

	mygo.App.WhenReady(func() {
		sink.debugf("pingtunnel %s · %s", appVersion, platformName())
		sink.debugf("设置文件：%s", shortPath(store.Path()))
		if bin := loadBinary(tunnel.Settings().BinaryPath); bin.err != nil {
			sink.debugf("%s", bin.err.Error())
		} else {
			sink.debugf("客户端：%s（%d 个可用参数）", shortPath(bin.path), len(bin.flags))
		}

		win := mygo.NewWindow(mygo.WindowOptions{
			Title:           "pingtunnel",
			URL:             "/",
			Width:           1040,
			Height:          760,
			MinWidth:        880,
			MinHeight:       620,
			TitleBarStyle:   mygo.TitleBarHidden,
			TitleBarHeight:  44,
			BackgroundColor: "light-dark(#eceef0, #0c0e10)",
			StateKey:        "main",
		})
		tunnel.attach(win)
		tunnel.emitStatus()

		// Closing the window leaves the tunnel running in the tray, as the
		// Qt client did; Quit from the tray really exits.
		win.OnClose(func(e *mygo.CloseEvent) {
			if quitting {
				return
			}
			e.PreventDefault()
			win.Hide()
		})

		menu := mygo.NewMenu([]*mygo.MenuItem{
			{Label: "打开工作台", Click: func(*mygo.MenuItem, *mygo.Window) { win.Show(); win.Focus() }},
			mygo.Separator(),
			{Label: "启动隧道", Click: func(*mygo.MenuItem, *mygo.Window) { go tunnel.Start(tunnel.Settings()) }},
			{Label: "停止隧道", Click: func(*mygo.MenuItem, *mygo.Window) { go tunnel.Stop() }},
			mygo.Separator(),
			{Label: "退出", Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
		})
		tray, err := mygo.NewTray(mygo.TrayOptions{
			Icon:           trayIcon,
			IconIsTemplate: true,
			ToolTip:        "pingtunnel",
			Menu:           menu,
		})
		if err != nil {
			log.Println("tray:", err)
		} else {
			tray.OnClick(func() { win.Show(); win.Focus() })
		}
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
