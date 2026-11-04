package cli

import (
	"fmt"

	"github.com/ps2build/tools/internal/ps2pack/pack"
	"github.com/spf13/cobra"
)

var verbose bool

var rootCmd = &cobra.Command{
	Use:  "ps2pack <in_elf> <out_elf>",
	Long: "ps2pack <in_elf> <out_elf>\n\nThird-party licenses: see licenses/tools/ in the source repository (github.com/ps2build/tools).",
	Args: cobra.ExactArgs(2),
	RunE: run,
}

func Execute() error {
	rootCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "verbose mode")
	return rootCmd.Execute()
}

func run(cmd *cobra.Command, args []string) error {
	in, out := args[0], args[1]

	if verbose {
		fmt.Printf("Packing %s...\n", in)
	}

	res, err := pack.Pack(in, out)
	if err != nil {
		return err
	}

	if verbose {
		fmt.Printf("%d segment(s), decompressed: %d bytes, compressed: %d bytes\n", res.SegmentCount, res.DecompressedSz, res.CompressedSz)
	}
	ratio := 100.0 * (1.0 - float64(res.OutputSize)/float64(res.InputSize))
	fmt.Printf("File compressed, from %d to %d bytes, ratio = %.2f%%\n", res.InputSize, res.OutputSize, ratio)

	return nil
}
