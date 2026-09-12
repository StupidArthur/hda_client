package main

import (
	"embed"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

const version = "v1.2.1"

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	path := flag.String("config", "", "path to preset config.yaml")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Printf("HDA Mocker 4 %s\n", version)
		return
	}
	if *path != "" {
		runCLI(*path)
		return
	}
	app := NewApp()
	if err := wails.Run(&options.App{
		Title:            "HDA Mocker 4",
		Width:            1180,
		Height:           780,
		MinWidth:         920,
		MinHeight:        620,
		BackgroundColour: &options.RGBA{R: 247, G: 246, B: 243, A: 255},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind:             []interface{}{app},
	}); err != nil {
		log.Printf("start desktop application: %v", err)
	}
}

func runCLI(configPath string) {
	_, _, err := loadConfig(configPath)
	if err != nil {
		log.Fatal(err)
	}
	controller := NewRuntimeController()
	if err := controller.StartConfig(configPath); err != nil {
		log.Fatal(err)
	}
	for {
		state := controller.Snapshot()
		if state.Phase == phaseRunning {
			break
		}
		if state.Phase == phaseFailed || state.Phase == phaseIdle {
			log.Fatal(state.Message)
		}
		time.Sleep(50 * time.Millisecond)
	}
	log.Printf("HDA Mocker 4 %s running; press Ctrl+C to stop", version)
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
	controller.StopAndWait(30 * time.Second)
}
