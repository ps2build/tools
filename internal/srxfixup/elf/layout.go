package elf

import "sort"

// AdjustAlign rounds value up to the next multiple of align (align must
// be a power of two).
func AdjustAlign(value, align uint32) uint32 {
	if align == 0 {
		return value
	}
	return ^(align - 1) & (align + value - 1)
}

// ReorderSymtab reorders every SYMTAB/DYNSYM section's symbols as
//
//	[null] [one STT_SECTION symbol per real section, in section order]
//	       [remaining local (bind==0) symbols, original order]
//	       [everything else, original order]
//
// dropping any now-orphaned unnamed STT_SECTION symbol that didn't match
// a real section, and sets sh_info to the resulting local/global
// boundary (the ELF convention SHT_SYMTAB's sh_info encodes).
func ReorderSymtab(f *File) {
	for _, s := range f.Sections[1:] {
		if s.Shdr.Type == SHTSymTab || s.Shdr.Type == SHTDynSym {
			reorderSymtabSection(f, s)
		}
	}
}

func reorderSymtabSection(f *File, s *Section) {
	old := append([]*Symbol(nil), s.Symbols...)
	newtab := make([]*Symbol, 0, len(old))
	newtab = append(newtab, old[0]) // null symbol
	used := make([]bool, len(old))

	for _, sc := range f.Sections[1:] {
		for i := 1; i < len(old); i++ {
			sym := old[i]
			if used[i] || sym == nil {
				continue
			}
			if sym.Type == STTSection && sym.Name == "" && sym.Section != nil && sym.Section.Name == sc.Name {
				newtab = append(newtab, sym)
				used[i] = true
				break
			}
		}
	}
	for i := 1; i < len(old); i++ {
		if !used[i] && old[i] != nil && old[i].Type == STTSection && old[i].Name == "" {
			used[i] = true // orphan section symbol, dropped
		}
	}
	for i := 1; i < len(old); i++ {
		if !used[i] && old[i] != nil && old[i].Bind == STBLocal {
			newtab = append(newtab, old[i])
			used[i] = true
		}
	}
	s.Shdr.Info = uint32(len(newtab))
	for i := 1; i < len(old); i++ {
		if !used[i] && old[i] != nil {
			newtab = append(newtab, old[i])
		}
	}
	s.Symbols = newtab
	s.Shdr.Size = s.Shdr.Entsize * uint32(len(newtab))
}

// RebuildSectionNameStrings rewrites f.ShStr's data from every real
// section's current Name.
func RebuildSectionNameStrings(f *File) {
	if f.Sections == nil {
		return
	}
	var buf []byte
	buf = append(buf, 0)
	for _, s := range f.Sections[1:] {
		s.Shdr.Name = uint32(len(buf))
		buf = append(buf, []byte(s.Name)...)
		buf = append(buf, 0)
	}
	f.ShStr.Data = buf
	f.ShStr.Shdr.Size = uint32(len(buf))
}

// RebuildSymbolNameStrings rewrites every SYMTAB/DYNSYM section's linked
// strtab from the current symbol names, reusing an identical existing
// entry instead of duplicating it.
func RebuildSymbolNameStrings(f *File) {
	if f.Sections == nil {
		return
	}
	for _, s := range f.Sections[1:] {
		if s.Shdr.Type == SHTSymTab || s.Shdr.Type == SHTDynSym {
			rebuildSymbolNameStringsSection(s)
		}
	}
}

func rebuildSymbolNameStringsSection(s *Section) {
	strtab := s.Link
	var buf []byte
	buf = append(buf, 0)
	for i := 1; i < len(s.Symbols); i++ {
		sym := s.Symbols[i]
		if sym == nil || sym.Name == "" {
			continue
		}
		if idx := searchStringTable(buf, sym.Name); idx != 0 {
			sym.Sym.Name = idx
			continue
		}
		sym.Sym.Name = uint32(len(buf))
		buf = append(buf, []byte(sym.Name)...)
		buf = append(buf, 0)
	}
	strtab.Data = buf
	strtab.Shdr.Size = uint32(len(buf))
}

func searchStringTable(tbl []byte, str string) uint32 {
	idx := 1
	for idx < len(tbl) {
		end := idx
		for end < len(tbl) && tbl[end] != 0 {
			end++
		}
		if string(tbl[idx:end]) == str {
			return uint32(idx)
		}
		idx = end + 1
	}
	return 0
}

// File-order-list slot types. Exported - convert.LayoutSrxFile needs to
// search and selectively consume individual slots by its own
// config-driven criteria, unlike WriteFile's own simpler, generic layout
// path.
const (
	EFSNone = iota
	EFSElfHeader
	EFSProgramHeaderTable
	EFSProgramHeaderEntry
	EFSSectionHeaderTable
	EFSSectionData
	EFSEnd
)

type FileSlot struct {
	Offset uint32
	Size   uint32
	Align  uint32
	Type   int
	Php    *ProgHeader // for EFSProgramHeaderEntry
	Scp    *Section    // for EFSSectionData
}

// BuildFileOrderList lays out every file-resident piece (header, program
// header table + one entry per segment, section header table, and every
// section not already claimed by a PT_LOAD/PT_MIPS_REGINFO/PT_SCE_*MOD
// segment) by its current on-disk offset.
func BuildFileOrderList(f *File) []FileSlot {
	claimed := make(map[*Section]bool)
	var slots []FileSlot
	slots = append(slots, FileSlot{Type: EFSElfHeader, Offset: 0, Size: 52, Align: 4})
	if len(f.Prog) > 0 {
		slots = append(slots, FileSlot{Type: EFSProgramHeaderTable, Offset: 52, Size: uint32(32 * len(f.Prog)), Align: 4})
		for i := range f.Prog {
			php := f.Prog[i]
			slots = append(slots, FileSlot{
				Type: EFSProgramHeaderEntry, Php: php,
				Offset: php.Phdr.Offset, Size: php.Phdr.Filesz, Align: php.Phdr.Align,
			})
			for _, s := range php.Sections {
				claimed[s] = true
			}
		}
	}
	shoff := f.Ehdr.Shoff
	if shoff == 0 {
		shoff = 0xFFFFFF00 // -256, matching (Elf32_Off)(-256) sentinel
	}
	slots = append(slots, FileSlot{Type: EFSSectionHeaderTable, Offset: shoff, Size: uint32(40 * len(f.Sections)), Align: 4})
	for _, s := range f.Sections[1:] {
		if !claimed[s] {
			slots = append(slots, FileSlot{Type: EFSSectionData, Scp: s, Offset: s.Shdr.Offset, Size: s.Shdr.Size, Align: s.Shdr.Addralign})
		}
	}
	sort.SliceStable(slots, func(i, j int) bool { return compFileSlot(&slots[i], &slots[j]) < 0 })
	return slots
}

func compFileSlot(p1, p2 *FileSlot) int {
	rank := func(s *FileSlot) int {
		switch s.Type {
		case EFSElfHeader:
			return 0
		case EFSProgramHeaderTable:
			return 1
		default:
			return 2
		}
	}
	r1, r2 := rank(p1), rank(p2)
	if r1 != r2 {
		return r1 - r2
	}
	if r1 < 2 {
		return 0
	}
	// Program header entries sort before the section header table at an
	// equal offset.
	if p1.Type == EFSProgramHeaderEntry && p2.Type == EFSSectionHeaderTable {
		return -1
	}
	if p1.Type == EFSSectionHeaderTable && p2.Type == EFSProgramHeaderEntry {
		return 1
	}
	if p1.Offset == p2.Offset {
		return 0
	}
	if p1.Offset < p2.Offset {
		return -1
	}
	return 1
}

// ShrinkFileOrderList reassigns each slot a fresh, tightly-packed offset
// (respecting alignment) in list order, removing whatever gaps/overlaps
// the previous layout had.
func ShrinkFileOrderList(slots []FileSlot) {
	var pos uint32
	for i := range slots {
		off := AdjustAlign(pos, slots[i].Align)
		slots[i].Offset = off
		pos = off + slots[i].Size
	}
}

// WritebackFileOrderList commits the slots' final offsets back into the
// real Ehdr/Phdr/Shdr fields.
func WritebackFileOrderList(f *File, slots []FileSlot) {
	for i := range slots {
		s := &slots[i]
		switch s.Type {
		case EFSProgramHeaderTable:
			f.Ehdr.Phoff = s.Offset
		case EFSProgramHeaderEntry:
			s.Php.Phdr.Offset = s.Offset
			segoff := s.Offset
			if len(s.Php.Sections) > 0 {
				s.Php.Sections[0].Shdr.Offset = s.Offset
				for i := 1; i < len(s.Php.Sections); i++ {
					sec := s.Php.Sections[i]
					if sec.Shdr.Type != SHTNoBits {
						segoff = sec.Shdr.Addr + s.Offset - s.Php.Sections[0].Shdr.Addr
					}
					sec.Shdr.Offset = segoff
					if sec.Shdr.Type != SHTNoBits {
						segoff += sec.Shdr.Size
					}
				}
			}
		case EFSSectionHeaderTable:
			f.Ehdr.Shoff = s.Offset
		case EFSSectionData:
			s.Scp.Shdr.Offset = s.Offset
		}
	}
}
