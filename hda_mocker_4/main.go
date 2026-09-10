package main

import (
	"flag"
	"fmt"
	"log"
)

const version = "v1.0.2"

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Printf("HDA Mocker 4 version=%s", version)
	path := flag.String("config", "", "path to preset config.yaml")
	flag.Parse()
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
