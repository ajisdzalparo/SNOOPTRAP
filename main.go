package main

import (
	"embed"
	"os"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Automatically grant camera and microphone permissions in WebView2 without prompts
	_ = os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", "--use-fake-ui-for-media-stream --autoplay-policy=no-user-gesture-required")

	app := NewApp()

	isAutoStart := false
	for _, arg := range os.Args[1:] {
		if strings.EqualFold(strings.TrimSpace(arg), "--autostart") {
			isAutoStart = true
			break
		}
	}
	// Start hidden in system tray on Windows boot when ARMED or DISARMED,
	// EXCEPT when TRIGGERED (immediately show fullscreen PIN Lockbox!)
	startHidden := isAutoStart && app.InitialStatus() != "TRIGGERED"

	err := wails.Run(&options.App{
		Title:             "SnoopTrap // IntruderLens",
		Width:             1024,
		Height:            768,
		MinWidth:          800,
		MinHeight:         600,
		StartHidden:       startHidden,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 5, G: 5, B: 5, A: 255},
		HideWindowOnClose: false,
		OnStartup:        app.startup,
		OnBeforeClose:    app.beforeClose,
		Bind: []interface{}{
			app,
		},
		Windows: &windows.Options{
			WebviewIsTransparent:              false,
			WindowIsTranslucent:               false,
			BackdropType:                      windows.None,
			DisableWindowIcon:                 false,
			DisableFramelessWindowDecorations: false,
			WebviewUserDataPath:               "",
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}

