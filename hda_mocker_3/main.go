package main

import (
	"fmt"
	"log"
	"os"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	arg := "default"
	if len(os.Args) > 2 {
		fmt.Fprintln(os.Stderr, "usage: hda_mocker_3.exe <preset-name|config.yaml>")
		os.Exit(2)
	}
	if len(os.Args) == 2 {
		arg = os.Args[1]
	}
	s, path, err := loadSettings(arg)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("using %s", path)
	if err := runServer(s); err != nil {
		log.Fatal(err)
	}
}
