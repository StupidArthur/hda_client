package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const version = "v1.4.2"

func main() {
	os.Exit(run())
}

func run() int {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	configPath := flag.String("config", "", "path to preset config.yaml")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "HDA Mocker 4 %s\n\n", version)
		fmt.Fprintln(flag.CommandLine.Output(), "Usage:")
		fmt.Fprintln(flag.CommandLine.Output(), "  hda_mocker_4.exe --config <path-to-config.yaml>")
		fmt.Fprintln(flag.CommandLine.Output(), "  hda_mocker_4.exe --version")
		fmt.Fprintln(flag.CommandLine.Output(), "\nThe configuration directory is the preset root; it must contain hda/, da/, and runtime/ may be created automatically.")
	}
	flag.Parse()
	if *showVersion {
		fmt.Printf("HDA Mocker 4 %s\n", version)
		return 0
	}
	if *configPath == "" {
		flag.Usage()
		return 2
	}
	if flag.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "unexpected arguments: %v\n", flag.Args())
		flag.Usage()
		return 2
	}

	logs, err := initializeApplicationLogs()
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: persistent logging unavailable: %v\n", err)
	} else {
		defer logs.Close()
		if logs.crashErr != nil {
			log.Printf("WARNING: crash logging unavailable: %v", logs.crashErr)
		}
	}
	log.Printf("HDA Mocker 4 %s starting", version)
	log.Printf("config: %s", *configPath)
	return runCLI(*configPath)
}

func runCLI(configPath string) int {
	log.Printf("startup: validating configuration and datasets")
	if _, _, err := loadConfig(configPath); err != nil {
		log.Printf("startup failed: %v", err)
		return 1
	}

	controller := NewRuntimeController()
	if err := controller.StartConfig(configPath); err != nil {
		log.Printf("startup failed: %v", err)
		return 1
	}

	lastPhase, lastMessage := "", ""
	for {
		state := controller.Snapshot()
		if state.Phase != lastPhase || state.Message != lastMessage {
			log.Printf("phase=%s message=%s hda_files=%d/%d samples=%d tags=%d da_tags=%d",
				state.Phase, state.Message, state.HDAFilesDone, state.HDAFilesTotal,
				state.HDASamples, state.TagCount, state.DATagCount)
			lastPhase, lastMessage = state.Phase, state.Message
		}
		if state.Phase == phaseRunning {
			break
		}
		if state.Phase == phaseFailed || state.Phase == phaseIdle {
			log.Printf("startup failed: %s", state.Message)
			controller.StopAndWait(30 * time.Second)
			return 1
		}
		time.Sleep(100 * time.Millisecond)
	}

	log.Printf("service running at %s; press Ctrl+C to stop", controller.Snapshot().Endpoint)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Printf("shutdown requested")
	controller.StopAndWait(30 * time.Second)
	log.Printf("shutdown complete")
	return 0
}

func initializeApplicationLogs() (*applicationLogs, error) {
	logs, err := openApplicationLogs()
	if err != nil {
		return nil, err
	}
	log.SetOutput(io.MultiWriter(os.Stdout, logs.rolling))
	return logs, nil
}
