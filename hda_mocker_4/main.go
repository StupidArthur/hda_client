package main

import (
	"flag"
	"fmt"
	"log"
)

const version = "v1.0.4"

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	path := flag.String("config", "", "path to preset config.yaml")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Printf("HDA Mocker 4 %s\n", version)
		return
	}
	log.Printf("HDA Mocker 4 version=%s", version)
	if *path == "" {
		log.Fatal("usage: hda_mocker_4 --config presets/demo/config.yaml")
	}
	cfg, root, e := loadConfig(*path)
	if e != nil {
		log.Fatal(e)
	}
	if e = run(cfg, root); e != nil {
		log.Fatal(e)
	}
}

var _ = fmt.Sprintf
