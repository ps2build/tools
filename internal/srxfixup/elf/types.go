package elf

// On-disk ELF32 structures, read/written little-endian directly
// (encoding/binary) - every host this runs on and the target ELF class
// (ELFData2LSB) are both little-endian, so no byte-swap path is needed.

type Ehdr struct {
	Ident     [16]byte
	Type      uint16
	Machine   uint16
	Version   uint32
	Entry     uint32
	Phoff     uint32
	Shoff     uint32
	Flags     uint32
	Ehsize    uint16
	Phentsize uint16
	Phnum     uint16
	Shentsize uint16
	Shnum     uint16
	Shstrndx  uint16
}

type Phdr struct {
	Type   uint32
	Offset uint32
	Vaddr  uint32
	Paddr  uint32
	Filesz uint32
	Memsz  uint32
	Flags  uint32
	Align  uint32
}

type Shdr struct {
	Name      uint32
	Type      uint32
	Flags     uint32
	Addr      uint32
	Offset    uint32
	Size      uint32
	Link      uint32
	Info      uint32
	Addralign uint32
	Entsize   uint32
}

type SymRaw struct {
	Name  uint32
	Value uint32
	Size  uint32
	Info  uint8
	Other uint8
	Shndx uint16
}

type RelRaw struct {
	Offset uint32
	Info   uint32
}

type IopMod struct {
	ModuleInfo    uint32
	Entry         uint32
	GPValue       uint32
	TextSize      uint32
	DataSize      uint32
	BssSize       uint32
	ModuleVersion uint16
	ModuleName    string
}

type EeMod struct {
	ModuleInfo    uint32
	Entry         uint32
	GPValue       uint32
	TextSize      uint32
	DataSize      uint32
	BssSize       uint32
	ErxLibAddr    uint32
	ErxLibSize    uint32
	ErxStubAddr   uint32
	ErxStubSize   uint32
	ModuleVersion uint16
	ModuleName    string
}

type File struct {
	Ehdr     Ehdr
	Sections []*Section // index 0 is the null section, matching real ELF section numbering
	ShStr    *Section
	Prog     []*ProgHeader
}

type Section struct {
	Shdr   Shdr
	Data   []byte
	Name   string
	Number int
	Link   *Section
	Info   *Section

	// Populated only for SHT_SYMTAB/SHT_DYNSYM sections.
	Symbols []*Symbol
	// Populated only for SHT_REL sections.
	Relocs []*Reloc
	// Populated only for the SHT_MIPS_DEBUG section, if present.
	MipsDebug *MipsSymbolic

	// Populated only for SHT_SCE_IOPMOD/SHT_SCE_EEMOD sections - the
	// parsed module-info struct, kept in sync with Shdr.Size via
	// SetIopMod/SetEeMod.
	iopMod *IopMod
	eeMod  *EeMod

	// The section's original sh_addr/gp value before srx layout moved
	// it, needed by R_MIPS_GPREL16/LITERAL rebuild.
	OrgAddr    uint32
	OrgGPValue uint32
}

type Symbol struct {
	Sym      SymRaw
	Name     string
	Number   int
	Bind     int
	Type     int
	Section  *Section // resolved from Sym.Shndx when it's a real section index
	RefCount int
}

type Reloc struct {
	Rel    RelRaw
	Symbol *Symbol
	Type   int
}

type ProgHeader struct {
	Phdr     Phdr
	Sections []*Section // php.scp - sections belonging to this segment
}

// IsDefined reports whether s's section index is a real section
// (<=0xFEFF) or SHN_ABS.
func (s *Symbol) IsDefined() bool {
	if s == nil {
		return false
	}
	if s.Sym.Shndx == SHNUndef {
		return false
	}
	if s.Sym.Shndx <= 0xFEFF {
		return true
	}
	return s.Sym.Shndx == SHNAbs
}

// Value returns s's value; for ET_REL files, a section-relative symbol's
// value is offset by the section's current sh_addr.
func (s *Symbol) Value(f *File) uint32 {
	if !s.IsDefined() {
		return 0
	}
	if s.Sym.Shndx != SHNAbs && f.Ehdr.Type == ETRel {
		return s.Section.Shdr.Addr + s.Sym.Value
	}
	return s.Sym.Value
}

func (f *File) SearchSection(shtype uint32) *Section {
	for _, s := range f.Sections[1:] {
		if s.Shdr.Type == shtype {
			return s
		}
	}
	return nil
}

func (f *File) SearchSectionByName(name string) *Section {
	for _, s := range f.Sections[1:] {
		if s.Name == name {
			return s
		}
	}
	return nil
}

func (f *File) SearchGlobalSymbol(name string) *Symbol {
	scp := f.SearchSection(SHTSymTab)
	if scp == nil {
		return nil
	}
	for _, sym := range scp.Symbols[1:] {
		if sym.Name == name && sym.Bind == STBGlobal {
			return sym
		}
	}
	return nil
}

// RemoveSection removes and returns the first section matching shtype,
// or nil if none.
func (f *File) RemoveSection(shtype uint32) *Section {
	for i := 1; i < len(f.Sections); i++ {
		if f.Sections[i].Shdr.Type == shtype {
			rm := f.Sections[i]
			f.Sections = append(f.Sections[:i], f.Sections[i+1:]...)
			f.Ehdr.Shnum = uint16(len(f.Sections))
			return rm
		}
	}
	return nil
}

func (f *File) RemoveSectionByName(name string) *Section {
	for i := 1; i < len(f.Sections); i++ {
		if f.Sections[i].Name == name {
			rm := f.Sections[i]
			f.Sections = append(f.Sections[:i], f.Sections[i+1:]...)
			f.Ehdr.Shnum = uint16(len(f.Sections))
			return rm
		}
	}
	return nil
}

// AddSection appends a new section and gives it a matching STT_SECTION
// symbol table entry.
func (f *File) AddSection(s *Section) {
	f.Sections = append(f.Sections, s)
	f.Ehdr.Shnum = uint16(len(f.Sections))
	f.AddSymbol("", 0, STTSection, 0, s, 0)
}

func (f *File) AddSymbol(name string, bind, typ int, value int, scp *Section, stShndx uint16) *Symbol {
	symtab := f.SearchSection(SHTSymTab)
	if symtab == nil {
		return nil
	}
	sym := &Symbol{
		Bind: bind,
		Type: typ,
	}
	if name != "" {
		sym.Name = name
	}
	sym.Sym.Info = uint8((typ & 0xF) + 16*bind)
	sym.Sym.Value = uint32(value)
	sym.Section = scp
	if scp != nil {
		sym.Sym.Shndx = 1
	} else {
		sym.Sym.Shndx = stShndx
	}
	symtab.Symbols = append(symtab.Symbols, sym)
	symtab.Shdr.Size += symtab.Shdr.Entsize
	return sym
}

// GetSectionData finds the PROGBITS section containing addr and returns
// a byte slice starting at that offset.
func (f *File) GetSectionData(addr uint32) []byte {
	for _, s := range f.Sections[1:] {
		if s.Shdr.Type == SHTProgBits && addr >= s.Shdr.Addr && addr < s.Shdr.Addr+s.Shdr.Size {
			return s.Data[addr-s.Shdr.Addr:]
		}
	}
	return nil
}
