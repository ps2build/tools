// Package pack implements ps2pack's container format:
package pack

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"fmt"

	"github.com/ps2build/tools/internal/ps2pack/elfimg"
	"github.com/ulikunitz/xz/lzma"
)

//go:embed stub.elf
var stubELF []byte

const (
	packedHeaderSize  = 8  // { original_entry, segment_count }
	packedSegmentSize = 16 // { decompressed_size, bss_size, load_addr, compressed_size }
	defaultAlign      = 16
)

// Result summarizes a completed pack, for CLI reporting.
type Result struct {
	InputSize      int
	OutputSize     int
	SegmentCount   int
	DecompressedSz int
	CompressedSz   int
}

// lzmaCompress produces the per-segment compressed form the stub's
// LzmaDecode call expects: the classic 5-byte LZMA props+dictsize header
// immediately followed by the compressed stream, with an explicit
// end-of-stream marker (the decoder is told to expect one via
// LZMA_FINISH_END, not just a byte counter).
//
// ulikunitz/xz/lzma's Writer always emits the fuller 13-byte ".lzma file"
// header (props + dictsize + an 8-byte uncompressed-size field); since our
// own PackedSegment already carries decompressed_size, that size field is
// redundant here, so it's stripped: bytes [0:5] are the props+dictsize
// header the stub's LzmaDecode call wants, bytes [13:] are the actual
// compressed stream.
func lzmaCompress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	dictCap := int(lzma.EncodeDictCap(int64(len(data))))
	dictCapBytes, err := lzma.DecodeDictCap(byte(dictCap))
	if err != nil || dictCapBytes < lzma.MinDictCap {
		dictCapBytes = lzma.MinDictCap
	}
	// Match the reference encoder's props.reduceSize = source_size: don't
	// declare a dictionary bigger than the data actually needs.
	cfg := lzma.WriterConfig{EOSMarker: true, DictCap: int(dictCapBytes)}
	w, err := cfg.NewWriter(&buf)
	if err != nil {
		return nil, fmt.Errorf("lzma writer: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return nil, fmt.Errorf("lzma compress: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("lzma close: %w", err)
	}
	full := buf.Bytes()
	if len(full) < 13 {
		return nil, fmt.Errorf("lzma output too short: %d bytes", len(full))
	}
	out := make([]byte, 0, 5+len(full)-13)
	out = append(out, full[:5]...)
	out = append(out, full[13:]...)
	return out, nil
}

// Pack reads inputPath's PT_LOAD segments, LZMA-compresses each
// independently, and writes a self-contained bootable ELF to outputPath.
func Pack(inputPath, outputPath string) (*Result, error) {
	img, err := elfimg.ReadSegments(inputPath)
	if err != nil {
		return nil, err
	}

	stub, err := elfimg.ReadStub(stubELF)
	if err != nil {
		return nil, err
	}

	le := binary.LittleEndian

	var payload bytes.Buffer
	hdrBuf := make([]byte, packedHeaderSize)
	le.PutUint32(hdrBuf[0:], img.Entry)
	le.PutUint32(hdrBuf[4:], uint32(len(img.Segments)))
	payload.Write(hdrBuf)

	var totalDecompressed, totalCompressed int
	for _, seg := range img.Segments {
		// Don't bother compressing a trailing run of zero bytes - just
		// remember how many there were and let the stub memset them
		// back in after decompressing.
		n := len(seg.Data)
		for n > 0 && seg.Data[n-1] == 0 {
			n--
		}
		bssSize := seg.BSSSize + uint32(len(seg.Data)-n)

		compressed, err := lzmaCompress(seg.Data[:n])
		if err != nil {
			return nil, err
		}

		segBuf := make([]byte, packedSegmentSize)
		le.PutUint32(segBuf[0:], uint32(n))
		le.PutUint32(segBuf[4:], bssSize)
		le.PutUint32(segBuf[8:], seg.LoadAddr)
		le.PutUint32(segBuf[12:], uint32(len(compressed)))
		payload.Write(segBuf)
		payload.Write(compressed)

		if pad := (-len(compressed)) & 3; pad != 0 {
			payload.Write(make([]byte, pad))
		}

		totalDecompressed += n
		totalCompressed += len(compressed)
	}

	payloadBytes := payload.Bytes()
	payloadAligned := alignUp(len(payloadBytes), defaultAlign)

	// Solve the segment's runtime placement so the stub - which uses
	// absolute addressing, no relocations - always lands at the fixed
	// address it was linked for (stub.LinkAddr), regardless of how big
	// the payload container is. See stub/linkfile.
	finalBase := stub.LinkAddr - uint32(payloadAligned)

	seg := make([]byte, payloadAligned+len(stub.FileBytes))
	copy(seg, payloadBytes) // remainder up to payloadAligned is zero padding
	copy(seg[payloadAligned:], stub.FileBytes)
	le.PutUint32(seg[uint32(payloadAligned)+stub.PayloadPtrOff:], finalBase)

	memSize := uint32(payloadAligned) + stub.MemSize
	out := elfimg.WriteSingleLoad(finalBase, stub.Entry, memSize, seg)

	if err := writeFile(outputPath, out); err != nil {
		return nil, err
	}

	inSize, err := fileSize(inputPath)
	if err != nil {
		return nil, err
	}

	return &Result{
		InputSize:      inSize,
		OutputSize:     len(out),
		SegmentCount:   len(img.Segments),
		DecompressedSz: totalDecompressed,
		CompressedSz:   totalCompressed,
	}, nil
}

func alignUp(n, align int) int {
	if r := n % align; r != 0 {
		n += align - r
	}
	return n
}
