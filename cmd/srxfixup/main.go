package main

import (
	"fmt"
	"os"

	"github.com/ps2build/tools/internal/srxfixup/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
