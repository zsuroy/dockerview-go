package main

import "os"

func SetColor() {
	if os.Getenv("TERM") != "xterm-256color" {
		os.Setenv("TERM", "xterm-256color")
	}
}
