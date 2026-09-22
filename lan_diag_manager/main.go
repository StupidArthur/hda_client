package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

const version = "v1.0.0"

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app, err := NewApp("")
	if err != nil {
		log.Fatal(err)
	}
	defer app.Close()
	if err := wails.Run(&options.App{
		Title: "局域网工具诊断中心", Width: 1280, Height: 820, MinWidth: 980, MinHeight: 640,
		BackgroundColour: &options.RGBA{R: 244, G: 246, B: 248, A: 255},
		AssetServer:      &assetserver.Options{Assets: assets}, OnStartup: app.startup,
		Bind: []interface{}{app},
	}); err != nil {
		log.Fatal(err)
	}
}
