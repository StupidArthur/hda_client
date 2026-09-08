package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"hda_client/internal/app"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	container := app.NewContainer()

	err := wails.Run(&options.App{
		Title:     "HDA 查询工具",
		Width:     1600,
		Height:    900,
		MinWidth:  960,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        container.Startup,
		OnShutdown:       container.Shutdown,
		Bind: []interface{}{
			container.Connection,
			container.Query,
			container.Settings,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}

