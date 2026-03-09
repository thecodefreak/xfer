package main

import (
	"os"

	"github.com/thecodefreak/xfer/cmd/xfer/commands"
)

func main() {
	if err := commands.Execute(); err != nil {
		os.Exit(1)
	}
}
