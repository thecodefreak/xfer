package main

import (
	"os"

	"xfer/cmd/xfer/commands"
)

func main() {
	if err := commands.Execute(); err != nil {
		os.Exit(1)
	}
}
