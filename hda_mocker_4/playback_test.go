package main

import (
	"testing"
)

func TestPlaybackCloseWithoutDA(t *testing.T) {
	p := newPlayback(nil, nil)
	p.Close()
}
