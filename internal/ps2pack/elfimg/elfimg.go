// Package elfimg reads a PS2 EE ELF's PT_LOAD segments (kept separate, one
// per input segment) and writes minimal single-segment EXEC ELFs of the
// shape ps2pack's stub expects to be loaded as (no section headers, one
// PT_LOAD).
package elfimg

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"fmt"
)

// Segment is one input PT_LOAD entry's file-backed bytes plus its BSS
// (memsz-filesz) requirement. Kept separate per segment, not flattened,
// matching pack.go's PackedSegment-per-PT_LOAD container shape.
type Segment struct {
	LoadAddr uint32
	Data     []byte // p_filesz bytes, exactly as they appear in the input file
	BSSSize  uint32 // p_memsz - p_filesz
}

// Image is an input ELF's entry point plus its PT_LOAD segments, in
// program-header order.
type Image struct {
	Entry    uint32
	Segments []Segment
}

// ReadSegments reads path's PT_LOAD segments with non-zero p_filesz, in
// program-header order. A PT_LOAD entry with p_filesz==0 (pure BSS) is
// skipped - there's nothing to compress.
func ReadSegments(path string) (*Image, error) {
	f, err := elf.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	if f.Class != elf.ELFCLASS32 || f.Machine != elf.EM_MIPS {
		return nil, fmt.Errorf("%s: not a 32-bit MIPS ELF", path)
	}

	img := &Image{Entry: uint32(f.Entry)}
	for _, p := range f.Progs {
		if p.Type != elf.PT_LOAD || p.Filesz == 0 {
			continue
		}
		data := make([]byte, p.Filesz)
		if _, err := p.ReadAt(data, 0); err != nil {
			return nil, fmt.Errorf("%s: read PT_LOAD at 0x%x: %w", path, p.Vaddr, err)
		}
		img.Segments = append(img.Segments, Segment{
			LoadAddr: uint32(p.Vaddr),
			Data:     data,
			BSSSize:  uint32(p.Memsz - p.Filesz),
		})
	}
	if len(img.Segments) == 0 {
		return nil, fmt.Errorf("%s: no PT_LOAD segments with file data", path)
	}
	return img, nil
}

// StubImage is the prebuilt EE stub's own ELF, parsed to recover the
// pieces ps2pack needs to embed and patch it.
type StubImage struct {
	Entry         uint32
	LinkAddr      uint32 // the stub's single PT_LOAD's natural p_vaddr
	FileBytes     []byte // p_filesz bytes
	MemSize       uint32 // p_memsz (>= len(FileBytes); the rest is the stub's own bss)
	PayloadPtrOff uint32 // offset of the g_payload_ptr symbol within FileBytes
}

// ReadStub parses a stub ELF (as produced by stub/ps2.yaml) from raw
// bytes, locating its PT_LOAD segment and the g_payload_ptr symbol the
// packer must patch at pack time.
func ReadStub(stubELF []byte) (*StubImage, error) {
	f, err := elf.NewFile(bytes.NewReader(stubELF))
	if err != nil {
		return nil, fmt.Errorf("parse stub ELF: %w", err)
	}
	defer f.Close()

	var load *elf.Prog
	for _, p := range f.Progs {
		if p.Type == elf.PT_LOAD {
			load = p
			break
		}
	}
	if load == nil {
		return nil, fmt.Errorf("stub ELF: no PT_LOAD segment")
	}

	data := make([]byte, load.Filesz)
	if _, err := load.ReadAt(data, 0); err != nil {
		return nil, fmt.Errorf("stub ELF: read PT_LOAD: %w", err)
	}

	syms, err := f.Symbols()
	if err != nil {
		return nil, fmt.Errorf("stub ELF: read symbols: %w", err)
	}
	var ptrAddr uint64
	found := false
	for _, s := range syms {
		if s.Name == "g_payload_ptr" {
			ptrAddr = s.Value
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("stub ELF: missing g_payload_ptr symbol")
	}

	return &StubImage{
		Entry:         uint32(f.Entry),
		LinkAddr:      uint32(load.Vaddr),
		FileBytes:     data,
		MemSize:       uint32(load.Memsz),
		PayloadPtrOff: uint32(ptrAddr) - uint32(load.Vaddr),
	}, nil
}

// ehdrSize + one Phdr, no section headers: a bare EXEC ELF with a single
// RWE PT_LOAD segment.
const (
	ehdrSize   = 52
	phdrSize   = 32
	headerSize = ehdrSize + phdrSize
)

// dataOffset is where the segment's bytes start in the file: the header preamble (ehdr + one phdr), rounded up to a 16-byte file offset.
const dataOffset = (headerSize + 0xF) &^ 0xF

// WriteSingleLoad builds a minimal ELF32 MIPS EXEC file with exactly one
// RWE PT_LOAD segment: vaddr/paddr = base, file contents = data, memsz =
// memSize (>= len(data); the difference is BSS the loader zero-fills).
func WriteSingleLoad(base, entry, memSize uint32, data []byte) []byte {
	out := make([]byte, dataOffset)

	copy(out[0:4], "\x7fELF")
	out[4] = 1 // ELFCLASS32
	out[5] = 1 // ELFDATA2LSB
	out[6] = 1 // EV_CURRENT
	le := binary.LittleEndian
	le.PutUint16(out[16:], uint16(elf.ET_EXEC))
	le.PutUint16(out[18:], uint16(elf.EM_MIPS))
	le.PutUint32(out[20:], 1) // e_version
	le.PutUint32(out[24:], entry)
	le.PutUint32(out[28:], ehdrSize) // e_phoff
	le.PutUint32(out[32:], 0)        // e_shoff
	le.PutUint32(out[36:], 0)        // e_flags
	le.PutUint16(out[40:], ehdrSize)
	le.PutUint16(out[42:], phdrSize)
	le.PutUint16(out[44:], 1) // e_phnum
	le.PutUint16(out[46:], 0) // e_shentsize
	le.PutUint16(out[48:], 0) // e_shnum
	le.PutUint16(out[50:], 0) // e_shstrndx

	ph := out[ehdrSize : ehdrSize+phdrSize]
	le.PutUint32(ph[0:], uint32(elf.PT_LOAD))
	le.PutUint32(ph[4:], dataOffset) // p_offset
	le.PutUint32(ph[8:], base)       // p_vaddr
	le.PutUint32(ph[12:], base)      // p_paddr
	le.PutUint32(ph[16:], uint32(len(data)))
	le.PutUint32(ph[20:], memSize)
	le.PutUint32(ph[24:], uint32(elf.PF_R|elf.PF_W|elf.PF_X))
	le.PutUint32(ph[28:], 0x10) // p_align

	return append(out, data...)
}
