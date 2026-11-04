package cli

import (
	"fmt"
	"os"

	dvpas "github.com/ps2build/tools/internal/dvpas"
	"github.com/spf13/cobra"
)

var outPath string

var rootCmd = &cobra.Command{
	Use:  "dvp-as <in.vsm> -o <out.o>",
	Long: "dvp-as <in.vsm> -o <out.o>\n\nThird-party licenses: see licenses/tools/ in the source repository (github.com/ps2build/tools).",
	Args: cobra.ExactArgs(1),
	RunE: run,
}

func Execute() error {
	rootCmd.Flags().StringVarP(&outPath, "output", "o", "", "output object file")
	_ = rootCmd.MarkFlagRequired("output")
	return rootCmd.Execute()
}

func run(cmd *cobra.Command, args []string) error {
	in, err := os.Open(args[0])
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close()

	if err := dvpas.Assemble(in, out); err != nil {
		return fmt.Errorf("%s: %w", args[0], err)
	}
	return nil
}
