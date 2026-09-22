//go:build windows

package main

import (
	"os"
	"syscall"
)

func redirectProcessStderr(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	os.Stderr = f
	syscall.Stderr = syscall.Handle(f.Fd())
	return f, nil
}
