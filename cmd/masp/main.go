package main

import (
	"os"

	"github.com/ps2build/tools/internal/masp/cli"
)

func main() {
	os.Exit(cli.Run(os.Args, os.Stdout, os.Stderr))
}
