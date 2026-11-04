package main

import (
	"fmt"
	"os"

	"github.com/ps2build/tools/pkg/ps2client/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "ps2client:", err)
		os.Exit(1)
	}
}
