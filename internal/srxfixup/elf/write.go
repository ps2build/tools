package elf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
)

// RenumberSymtab assigns each SYMTAB/DYNSYM section's symbols their
// final table index, so relocation writing can resolve rp.Symbol.Number
// instead of re-searching.
func RenumberSymtab(f *File) {
	for _, s := range f.Sections[1:] {
		if s.Shdr.Type == SHTSymTab || s.Shdr.Type == SHTDynSym {
			for i, sym := range s.Symbols {
				sym.Number = i
			}
		}
	}
}

func WriteFile(f *File, filename string) error {
	RenumberSymtab(f)

	if len(f.Sections) > 0 && f.Ehdr.Shentsize != 0 {
		for i, s := range f.Sections {
			s.Number = i
		}
		for _, s := range f.Sections {
			switch s.Shdr.Type {
			case SHTRela, SHTRel:
				if s.Info != nil {
					s.Shdr.Info = uint32(s.Info.Number)
				}
				fallthrough
			case SHTSymTab, SHTHash, SHTDynamic, SHTDynSym:
				if s.Link != nil {
					s.Shdr.Link = uint32(s.Link.Number)
				}
			}
			if s == f.ShStr {
				f.Ehdr.Shstrndx = uint16(s.Number)
			}
		}
	}

	w := &sparseFile{}

	if err := binary.Write(sectionWriterAt(w, 0), binary.LittleEndian, &f.Ehdr); err != nil {
		return err
	}

	if f.Ehdr.Phnum != 0 && f.Ehdr.Phentsize != 0 {
		off := int64(f.Ehdr.Phoff)
		for _, php := range f.Prog {
			if err := binary.Write(sectionWriterAt(w, off), binary.LittleEndian, &php.Phdr); err != nil {
				return err
			}
			off += 32
		}
	}

	if len(f.Sections) > 0 && f.Ehdr.Shentsize != 0 {
		off := int64(f.Ehdr.Shoff)
		for _, s := range f.Sections {
			if err := binary.Write(sectionWriterAt(w, off), binary.LittleEndian, &s.Shdr); err != nil {
				return err
			}
			off += 40
		}
		for _, s := range f.Sections {
			pos, size := s.Shdr.Offset, s.Shdr.Size
			if pos == 0 || size == 0 {
				continue
			}
			switch s.Shdr.Type {
			case SHTProgBits, SHTRela, SHTHash, SHTDynamic, SHTMipsRegInfo:
				w.writeAt(int64(pos), s.Data)
			case SHTSymTab, SHTDynSym:
				writeSymtab(w, s)
			case SHTNoBits:
			case SHTRel:
				writeRel(w, s)
			case SHTMipsDebug:
				if err := writeMipsSymbolic(w, pos, s.MipsDebug); err != nil {
					return err
				}
			case SHTSceIopMod:
				w.writeAt(int64(pos), marshalIopMod(s.iopMod))
			case SHTSceEeMod:
				w.writeAt(int64(pos), marshalEeMod(s.eeMod))
			default:
				w.writeAt(int64(pos), s.Data)
			}
		}
	}

	if err := os.WriteFile(filename, w.bytes(), 0o644); err != nil {
		return fmt.Errorf("%q can't open: %w", filename, err)
	}
	return nil
}

func writeSymtab(w *sparseFile, s *Section) {
	off := int64(s.Shdr.Offset)
	for _, sym := range s.Symbols {
		raw := sym.Sym
		if sym.Section != nil {
			raw.Shndx = uint16(sym.Section.Number)
		}
		var buf bytes.Buffer
		_ = binary.Write(&buf, binary.LittleEndian, &raw)
		w.writeAt(off, buf.Bytes())
		off += 16
	}
}

func writeRel(w *sparseFile, s *Section) {
	off := int64(s.Shdr.Offset)
	for _, rel := range s.Relocs {
		symNum := uint32(0)
		if rel.Symbol != nil {
			symNum = uint32(rel.Symbol.Number)
		} else if s.Info != nil {
			symNum = uint32(s.Info.Number)
		}
		raw := RelRaw{
			Offset: rel.Rel.Offset,
			Info:   (symNum << 8) | (uint32(rel.Type) & 0xFF),
		}
		var buf bytes.Buffer
		_ = binary.Write(&buf, binary.LittleEndian, &raw)
		w.writeAt(off, buf.Bytes())
		off += 8
	}
}

// sparseFile is a minimal growable byte buffer supporting WriteAt-style
// placement at arbitrary (possibly out-of-order, possibly gapped)
// offsets.
type sparseFile struct {
	buf []byte
}

func (s *sparseFile) ensureLen(n int) {
	if len(s.buf) < n {
		grown := make([]byte, n)
		copy(grown, s.buf)
		s.buf = grown
	}
}

func (s *sparseFile) writeAt(off int64, data []byte) {
	end := int(off) + len(data)
	s.ensureLen(end)
	copy(s.buf[off:end], data)
}

func (s *sparseFile) bytes() []byte { return s.buf }

// WriteAt implements io.WriterAt so writeMipsSymbolic can use it directly.
func (s *sparseFile) WriteAt(p []byte, off int64) (int, error) {
	s.writeAt(off, p)
	return len(p), nil
}

type offsetWriter struct {
	f   *sparseFile
	off int64
}

func (w *offsetWriter) Write(p []byte) (int, error) {
	w.f.writeAt(w.off, p)
	w.off += int64(len(p))
	return len(p), nil
}

func sectionWriterAt(f *sparseFile, off int64) *offsetWriter {
	return &offsetWriter{f: f, off: off}
}
