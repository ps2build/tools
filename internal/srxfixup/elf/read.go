package elf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
)

// ReadFile parses an ELF32/MIPS object (relocatable .o, or an
// already-converted SCE IOP/EE relexec module) into the working File
// representation.
func ReadFile(filename string) (*File, error) {
	raw, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("%s: can't open: %w", filename, err)
	}
	r := bytes.NewReader(raw)

	f := &File{}
	if err := binary.Read(r, binary.LittleEndian, &f.Ehdr); err != nil {
		return nil, fmt.Errorf("%s: could not read ELF header: %w", filename, err)
	}
	if !bytes.Equal(f.Ehdr.Ident[:4], []byte{0x7F, 'E', 'L', 'F'}) {
		return nil, fmt.Errorf("%s: not elf format", filename)
	}
	if f.Ehdr.Ident[4] != ELFClass32 || f.Ehdr.Ident[5] != ELFData2LSB || f.Ehdr.Ident[6] != EVCurrent {
		return nil, fmt.Errorf("%s: not 32-bit object or not little endian or invalid elf version", filename)
	}
	if f.Ehdr.Machine != EMMips {
		return nil, fmt.Errorf("%s: not EM_MIPS", filename)
	}
	if f.Ehdr.Phentsize != 0 && f.Ehdr.Phentsize != 32 {
		return nil, fmt.Errorf("%s: unknown program header size", filename)
	}
	if f.Ehdr.Shentsize != 0 && f.Ehdr.Shentsize != 40 {
		return nil, fmt.Errorf("%s: unknown section header size", filename)
	}

	if f.Ehdr.Phnum != 0 && f.Ehdr.Phentsize != 0 {
		f.Prog = make([]*ProgHeader, f.Ehdr.Phnum)
		pr := bytes.NewReader(raw[f.Ehdr.Phoff:])
		for i := range f.Prog {
			var ph Phdr
			if err := binary.Read(pr, binary.LittleEndian, &ph); err != nil {
				return nil, fmt.Errorf("%s: could not read ELF program header: %w", filename, err)
			}
			f.Prog[i] = &ProgHeader{Phdr: ph}
		}
	}

	count := int(f.Ehdr.Shnum)
	if count != 0 && f.Ehdr.Shentsize != 0 {
		f.Sections = make([]*Section, count)
		sr := bytes.NewReader(raw[f.Ehdr.Shoff:])
		for i := 0; i < count; i++ {
			var sh Shdr
			if err := binary.Read(sr, binary.LittleEndian, &sh); err != nil {
				return nil, fmt.Errorf("%s: could not read ELF section header: %w", filename, err)
			}
			f.Sections[i] = &Section{Shdr: sh, Number: i}
		}
		for i, s := range f.Sections {
			switch s.Shdr.Type {
			case SHTRela, SHTRel:
				s.Info = f.Sections[s.Shdr.Info]
				fallthrough
			case SHTSymTab, SHTHash, SHTDynamic, SHTDynSym:
				s.Link = f.Sections[s.Shdr.Link]
			}
			if i == int(f.Ehdr.Shstrndx) {
				f.ShStr = s
			}
		}

		// Pass 1: everything except SYMTAB/DYNSYM/NOBITS/REL (those need
		// section links resolved first, so they're handled below).
		for _, s := range f.Sections {
			pos, size := s.Shdr.Offset, s.Shdr.Size
			if pos == 0 || size == 0 {
				continue
			}
			switch s.Shdr.Type {
			case SHTProgBits, SHTRela, SHTHash, SHTDynamic, SHTMipsRegInfo:
				s.Data = sliceClamped(raw, pos, size)
			case SHTSymTab, SHTNoBits, SHTDynSym, SHTRel:
				// handled below
			case SHTMipsDebug:
				md, err := readMipsSymbolic(bytes.NewReader(raw), int64(pos))
				if err != nil {
					return nil, fmt.Errorf("%s: could not read ELF debug info: %w", filename, err)
				}
				s.MipsDebug = md
			case SHTSceIopMod:
				m, err := unmarshalIopMod(sliceClamped(raw, pos, size))
				if err != nil {
					return nil, err
				}
				s.iopMod = m
			case SHTSceEeMod:
				m, err := unmarshalEeMod(sliceClamped(raw, pos, size))
				if err != nil {
					return nil, err
				}
				s.eeMod = m
			default:
				// sh_size can legitimately exceed what's actually on disk
				// for an SHF_COMPRESSED section (e.g. DWARF .debug_info/
				// .debug_str under -gz): sh_size reflects the decompressed
				// size, real bytes on disk are shorter. This tool never
				// decompresses, so pad with zeros instead of slicing past
				// the file - the affected content is debug-only and never
				// affects a real module's loaded behavior.
				s.Data = sliceClamped(raw, pos, size)
			}
		}

		// Pass 2a: symbol tables (need shstrtab/link only).
		for i, s := range f.Sections {
			if (s.Shdr.Type == SHTSymTab || s.Shdr.Type == SHTDynSym) && s.Shdr.Offset != 0 && s.Shdr.Size != 0 {
				if err := readSymtab(f, i, raw); err != nil {
					return nil, err
				}
			}
		}
		// Pass 2b: relocations (need the linked symbol table already read).
		for i, s := range f.Sections {
			if s.Shdr.Type == SHTRel && s.Shdr.Offset != 0 && s.Shdr.Size != 0 {
				readRel(f, i, raw)
			}
		}
		// Section names, from shstrtab.
		for _, s := range f.Sections {
			if f.ShStr != nil {
				s.Name = cstr(f.ShStr.Data, s.Shdr.Name)
			}
		}
		// Program-header -> section membership.
		for i := range f.Prog {
			php := f.Prog[i]
			switch php.Phdr.Type {
			case PTLoad:
				assigned := make(map[*Section]bool)
				for _, s := range f.Sections[1:] {
					switch s.Shdr.Type {
					case SHTProgBits:
						if inRange(php.Phdr.Offset, php.Phdr.Filesz, s.Shdr.Offset) ||
							(s.Shdr.Size == 0 && s.Shdr.Offset == php.Phdr.Filesz+php.Phdr.Offset) {
							php.Sections = append(php.Sections, s)
							assigned[s] = true
						}
					case SHTNoBits:
						if inRange(php.Phdr.Vaddr, php.Phdr.Memsz, s.Shdr.Addr) ||
							(s.Shdr.Size == 0 && s.Shdr.Offset == php.Phdr.Filesz+php.Phdr.Offset) {
							php.Sections = append(php.Sections, s)
							assigned[s] = true
						}
					}
				}
			case PTMipsRegInfo:
				if s := f.SearchSection(SHTMipsRegInfo); s != nil {
					php.Sections = []*Section{s}
				}
			case PTSceIopMod:
				if s := f.SearchSection(SHTSceIopMod); s != nil {
					php.Sections = []*Section{s}
				}
			case PTSceEeMod:
				if s := f.SearchSection(SHTSceEeMod); s != nil {
					php.Sections = []*Section{s}
				}
			}
		}
		// Workaround for malformed files: make sh_addr deltas within a
		// PT_LOAD segment agree with sh_offset deltas.
		switch f.Ehdr.Type {
		case ETSceIopRelExec, ETSceIopRelExec2, ETSceEeRelExec, ETSceEeRelExec2:
			for _, php := range f.Prog {
				if php.Phdr.Type != PTLoad {
					continue
				}
				for i := 1; i < len(php.Sections); i++ {
					cur, prev := php.Sections[i], php.Sections[i-1]
					if cur.Shdr.Type == SHTNoBits {
						continue
					}
					addrDiff := cur.Shdr.Addr - prev.Shdr.Addr
					offDiff := cur.Shdr.Offset - prev.Shdr.Offset
					if addrDiff != offDiff {
						cur.Shdr.Addr = prev.Shdr.Addr + offDiff
					}
				}
			}
		}
	}

	return f, nil
}

// sliceClamped copies size bytes starting at pos from raw, zero-padding
// past the end of raw instead of panicking if size overruns it.
func sliceClamped(raw []byte, pos, size uint32) []byte {
	out := make([]byte, size)
	if int(pos) < len(raw) {
		copy(out, raw[pos:])
	}
	return out
}

func inRange(top, size, pos uint32) bool {
	return pos >= top && pos < size+top
}

func cstr(data []byte, off uint32) string {
	if data == nil || int(off) >= len(data) {
		return ""
	}
	end := off
	for int(end) < len(data) && data[end] != 0 {
		end++
	}
	return string(data[off:end])
}

func readSymtab(f *File, idx int, raw []byte) error {
	s := f.Sections[idx]
	entsize := s.Shdr.Entsize
	if entsize == 0 {
		return nil
	}
	n := s.Shdr.Size / entsize
	s.Symbols = make([]*Symbol, n)
	r := bytes.NewReader(raw[s.Shdr.Offset:])
	for i := uint32(0); i < n; i++ {
		var raw16 SymRaw
		if err := binary.Read(r, binary.LittleEndian, &raw16); err != nil {
			return fmt.Errorf("could not read ELF symbol table: %w", err)
		}
		sym := &Symbol{Sym: raw16}
		sym.Bind = int(raw16.Info >> 4)
		sym.Type = int(raw16.Info & 0xF)
		if raw16.Name != 0 && s.Link != nil {
			sym.Name = cstr(s.Link.Data, raw16.Name)
		}
		if raw16.Shndx != 0 && raw16.Shndx <= 0xFEFF {
			sym.Section = f.Sections[raw16.Shndx]
		}
		s.Symbols[i] = sym
	}
	return nil
}

func readRel(f *File, idx int, raw []byte) {
	s := f.Sections[idx]
	entsize := s.Shdr.Entsize
	if entsize == 0 {
		return
	}
	n := s.Shdr.Size / entsize
	s.Relocs = make([]*Reloc, n)
	r := bytes.NewReader(raw[s.Shdr.Offset:])
	var symbols []*Symbol
	if s.Link != nil {
		symbols = s.Link.Symbols
	}
	for i := uint32(0); i < n; i++ {
		var rr RelRaw
		_ = binary.Read(r, binary.LittleEndian, &rr)
		rel := &Reloc{Rel: rr, Type: int(rr.Info & 0xFF)}
		if symbols != nil {
			symIdx := rr.Info >> 8
			if int(symIdx) < len(symbols) {
				rel.Symbol = symbols[symIdx]
				rel.Symbol.RefCount++
			}
		}
		s.Relocs[i] = rel
	}
}
