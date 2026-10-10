// Command app is the Sexy Agent Tuner desktop application.
package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := &App{}
	err := wails.Run(&options.App{
		Title:            "Sexy Agent Tuner",
		Width:            1400,
		Height:           900,
		MinWidth:         900,
		MinHeight:        600,
		AssetServer:      &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 7, G: 6, B: 11, A: 255}, // --surface-0, no white flash on start
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind:             []any{app},
		Menu:             appMenu(app),
		// Wails only marks the window zoomable when Mac options are set; without
		// them the green button is greyed out and full screen can't be entered.
		Mac: &mac.Options{},
	})
	if err != nil {
		log.Fatal(err)
	}
}

// appMenu is the standard macOS menu bar. Edit makes ⌘C/⌘V work in inputs;
// View adds full screen, which Wails has no ready-made role for.
func appMenu(app *App) *menu.Menu {
	m := menu.NewMenu()
	m.Append(menu.AppMenu())
	m.Append(menu.EditMenu())
	view := m.AddSubmenu("View")
	view.AddText("Toggle Full Screen", &keys.Accelerator{Key: "f", Modifiers: []keys.Modifier{keys.CmdOrCtrlKey, keys.ControlKey}},
		func(*menu.CallbackData) {
			if runtime.WindowIsFullscreen(app.ctx) {
				runtime.WindowUnfullscreen(app.ctx)
			} else {
				runtime.WindowFullscreen(app.ctx)
			}
		})
	m.Append(menu.WindowMenu())
	return m
}
