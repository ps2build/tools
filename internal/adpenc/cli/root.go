package cli

import (
	"encoding/binary"
	"fmt"
	"os"

	"github.com/go-audio/wav"
	internal "github.com/ps2build/tools/internal/adpenc"
	"github.com/spf13/cobra"
)

var loop bool

var rootCmd = &cobra.Command{
	Use:  "adpenc <wav> <adp>",
	Long: "adpenc <wav> <adp>\n\nThird-party licenses: see licenses/tools/ in the source repository (github.com/ps2build/tools).",
	Args: cobra.ExactArgs(2),
	RunE: ConvertFile,
}

func Execute() error {
	rootCmd.Flags().BoolVarP(&loop, "loop", "L", false, "Loop audio")
	return rootCmd.Execute()
}

type AdpcmHeader struct {
	ID       [4]byte
	Version  uint8
	Channels uint8
	Loop     uint8
	Reserved uint8
	Pitch    uint32
	Samples  uint32
}

func ConvertFile(command *cobra.Command, args []string) error {
	file, err := os.Open(args[0])
	if err != nil {
		return fmt.Errorf("open wav: %w", err)
	}
	defer file.Close()

	decoder := wav.NewDecoder(file)
	if !decoder.IsValidFile() {
		return fmt.Errorf("not a valid wav file: %s", args[0])
	}
	fmt.Printf("Sample rate: %d Hz\n", decoder.SampleRate)
	fmt.Printf("Bit Depth: %d bits\n", decoder.BitDepth)
	fmt.Printf("Channels: %d\n", decoder.NumChans)

	audioBuf, err := decoder.FullPCMBuffer()
	if err != nil {
		return fmt.Errorf("decode pcm: %w", err)
	}

	numChans := int(decoder.NumChans)
	if numChans != 1 && numChans != 2 {
		return fmt.Errorf("unsupported channel count: %d", numChans)
	}
	opts := internal.Options{Loop: loop}

	// Independent per-channel streams, concatenated L then R for stereo.
	// NOT interleaved-per-block. If your playback engine expects
	// block-interleaved stereo (alternating L/R blocks for synchronized
	// streaming decode), this needs a different packer.
	var payload []byte
	for c := 0; c < numChans; c++ {
		opts.Channel = c
		enc, err := internal.Encode(audioBuf, opts)
		if err != nil {
			return fmt.Errorf("encode channel %d: %w", c, err)
		}
		payload = append(payload, enc...)
	}

	outFile, err := os.OpenFile(args[1], os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	defer outFile.Close()

	header := AdpcmHeader{
		ID:       [4]byte{'A', 'P', 'C', 'M'},
		Version:  1,
		Channels: uint8(numChans),
		Loop:     boolToInt(loop),
		Reserved: 0,
		Pitch:    uint32((decoder.SampleRate * 4096) / 48000),
		Samples:  uint32(audioBuf.NumFrames()),
	}

	if err := binary.Write(outFile, binary.LittleEndian, header); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	if _, err := outFile.Write(payload); err != nil {
		return fmt.Errorf("write payload: %w", err)
	}

	fmt.Printf("Wrote %s: %d bytes payload, %d blocks\n",
		args[1], len(payload), len(payload)/16)
	return nil
}

func boolToInt(b bool) uint8 {
	if b {
		return 1
	}
	return 0
}
