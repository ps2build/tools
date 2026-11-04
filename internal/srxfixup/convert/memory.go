package convert

import (
	"fmt"
	"os"

	"github.com/ps2build/tools/internal/srxfixup/config"
	"github.com/ps2build/tools/internal/srxfixup/elf"
)

func checkChangeBit(oldbit, newbit uint32) (up, down uint32) {
	up = ^oldbit & newbit & (newbit ^ oldbit)
	down = ^newbit & oldbit & (newbit ^ oldbit)
	return
}

func segmentStartSetup(segs []*config.SegConf, bitid uint32, moffset uint32) {
	for _, s := range segs {
		if s.BitID&bitid != 0 {
			s.Addr = moffset
			s.Size = 0
		}
	}
}

func addSectionToSegment(segs []*config.SegConf, scp *elf.Section, bitid uint32) {
	for _, s := range segs {
		if s.BitID&bitid != 0 {
			if len(s.Sections) == 0 {
				s.Addr = scp.Shdr.Addr
			}
			s.Sections = append(s.Sections, scp)
		}
	}
}

func segmentEndSetup(segs []*config.SegConf, bitid uint32, moffset *uint32, ee bool) {
	for _, s := range segs {
		if s.BitID&bitid != 0 {
			if ee && s.Name == "TEXT" {
				*moffset += 32
			}
			s.Size = *moffset - s.Addr
		}
	}
}

// layoutSrxMemory walks the config's ordered section-pattern list,
// assigning every allocatable section a fresh memory address in pattern
// order and grouping it into whichever segment(s) that pattern's entry
// belongs to.
func layoutSrxMemory(f *elf.File, tp *config.Table) error {
	if f.Sections == nil {
		return fmt.Errorf("no sections")
	}
	isEE := tp.Target == config.TargetEE
	scp := append([]*elf.Section(nil), f.Sections...)
	var moffset, oldbitid uint32

	for idx, odr := range tp.Sections {
		up, _ := checkChangeBit(oldbitid, odr.Flag)
		if up != 0 {
			segmentStartSetup(tp.Segments, up, moffset)
		}
		for s := 1; s < len(scp); s++ {
			if scp[s] != nil && sectNameMatch(odr.Pattern, scp[s].Name) == 0 && scp[s].Shdr.Flags&elf.SHFAlloc != 0 {
				moffset = elf.AdjustAlign(moffset, scp[s].Shdr.Addralign)
				scp[s].Shdr.Addr = moffset
				moffset += scp[s].Shdr.Size
				addSectionToSegment(tp.Segments, scp[s], odr.Flag)
				scp[s] = nil
			}
		}
		oldbitid = odr.Flag
		var nextFlag uint32
		if idx+1 < len(tp.Sections) {
			nextFlag = tp.Sections[idx+1].Flag
		}
		_, down := checkChangeBit(oldbitid, nextFlag)
		if down != 0 {
			segmentEndSetup(tp.Segments, down, &moffset, isEE)
		}
	}

	errCount := 0
	for s2 := 1; s2 < len(scp); s2++ {
		if scp[s2] == nil || scp[s2].Shdr.Flags&elf.SHFAlloc == 0 {
			continue
		}
		found := -1
		for s3 := 1; s3 < len(scp); s3++ {
			if scp[s3] != nil && (scp[s3].Shdr.Type == elf.SHTRela || scp[s3].Shdr.Type == elf.SHTRel) && scp[s3].Info == scp[s2] {
				found = s3
				break
			}
		}
		if found >= 0 {
			fmt.Fprintf(os.Stderr, "Error: section '%s' needs allocation and has relocation data but not in program segment\n", scp[s2].Name)
			scp[found] = nil
			errCount++
		} else {
			fmt.Fprintf(os.Stderr, "Warning: section '%s' needs allocation but not in program segment\n", scp[s2].Name)
			scp[s2] = nil
		}
	}
	if errCount == 0 {
		if err := updateModinfo(f, tp); err != nil {
			return err
		}
		updateMdebug(f)
		updateProgramheader(f, tp)
	}
	if errCount > 0 {
		return fmt.Errorf("%d section layout error(s)", errCount)
	}
	return nil
}

func updateModinfo(f *elf.File, tp *config.Table) error {
	text := tp.Segment("TEXT")
	data := tp.Segment("DATA")
	bss := tp.Segment("BSS")
	global := tp.Segment("GLOBALDATA")
	if text == nil || data == nil || bss == nil || global == nil {
		return fmt.Errorf("TEXT,DATA,BSS,GLOBALDATA segment missing")
	}
	if s := f.SearchSection(elf.SHTSceIopMod); s != nil {
		s.Shdr.Addr = 0
		m := s.IopMod()
		m.TextSize = data.Addr - text.Addr
		m.DataSize = bss.Addr - data.Addr
		m.BssSize = bss.Size
		m.GPValue = global.Addr + 0x7FF0
	}
	if s := f.SearchSection(elf.SHTSceEeMod); s != nil {
		s.Shdr.Addr = 0
		m := s.EeMod()
		m.TextSize = data.Addr - text.Addr
		m.DataSize = bss.Addr - data.Addr
		m.BssSize = bss.Size
		m.GPValue = global.Addr + 0x7FF0
	}
	return nil
}

func updateMdebug(f *elf.File) {
	if s := f.SearchSection(elf.SHTMipsDebug); s != nil {
		s.Shdr.Addr = 0
	}
}

func updateProgramheader(f *elf.File, tp *config.Table) {
	minsegalign := uint32(16)
	if tp.Target == config.TargetEE {
		minsegalign = 128
	}
	for n, ph := range tp.ProgramHeaderOrder {
		if ph.Type != config.PHTypeText || n >= len(f.Prog) {
			continue
		}
		var segs []*config.SegConf
		for _, name := range ph.SegmentNames {
			segs = append(segs, tp.Segment(name))
		}
		if len(segs) == 0 {
			continue
		}
		php := f.Prog[n]
		php.Phdr.Vaddr = segs[0].Addr
		align := minsegalign
		var allSects []*elf.Section
		for _, seg := range segs {
			allSects = append(allSects, seg.Sections...)
		}
		if len(allSects) == 0 {
			continue
		}
		php.Sections = allSects
		s := 0
		for s < len(allSects) && allSects[s].Shdr.Type == elf.SHTProgBits {
			if allSects[s].Shdr.Addralign > align {
				align = allSects[s].Shdr.Addralign
			}
			s++
		}
		if s > 0 {
			php.Phdr.Filesz = allSects[s-1].Shdr.Size + allSects[s-1].Shdr.Addr - allSects[0].Shdr.Addr
		}
		last := allSects[len(allSects)-1]
		php.Phdr.Memsz = last.Shdr.Size + last.Shdr.Addr - allSects[0].Shdr.Addr
		for ; s < len(allSects); s++ {
			if allSects[s].Shdr.Addralign > align {
				align = allSects[s].Shdr.Addralign
			}
		}
		php.Phdr.Align = align
	}
}

func removeUnuseSection(f *elf.File, tp *config.Table) {
	var toRemove []string
	for _, pattern := range tp.RemoveSectionList {
		for _, s := range f.Sections[1:] {
			if sectNameMatch(pattern, s.Name) == 0 {
				toRemove = append(toRemove, s.Name)
			}
		}
	}
	for _, name := range toRemove {
		f.RemoveSectionByName(name)
	}
}

func createReservedSymbols(f *elf.File, tp *config.Table) error {
	if f.SearchSection(elf.SHTSymTab) == nil {
		return fmt.Errorf("no symbol table")
	}
	for _, cs := range tp.CreateSymbols {
		sym := f.SearchGlobalSymbol(cs.Name)
		if sym == nil && (cs.Shindex > 0xFEFF ||
			(cs.Segment != nil && len(cs.Segment.Sections) > 0) ||
			(cs.Segment == nil && cs.SectName != "" && f.SearchSectionByName(cs.SectName) != nil)) {
			sym = f.AddSymbol(cs.Name, cs.Bind, 0, 0, nil, 0)
		}
		if sym == nil {
			continue
		}
		var shSize, shAddr uint32
		var scp *elf.Section
		if cs.Segment != nil {
			shAddr, shSize = cs.Segment.Addr, cs.Segment.Size
			if cs.Shindex <= 0xFEFF && len(cs.Segment.Sections) > 0 {
				scp = cs.Segment.Sections[0]
			}
		} else if cs.SectName != "" {
			scp = f.SearchSectionByName(cs.SectName)
			if scp != nil {
				shAddr, shSize = scp.Shdr.Addr, scp.Shdr.Size
			}
		}
		if cs.Segment != nil || scp != nil {
			sym.Bind = cs.Bind
			if sym.Type == 0 {
				sym.Type = cs.Type
			}
			sym.Sym.Info = uint8(((cs.Bind & 0xFF) << 4) + (cs.Type & 0xF))
			if cs.Shindex > 0xFEFF {
				sym.Sym.Shndx = uint16(cs.Shindex)
				sym.Section = nil
				switch cs.Seflag {
				case 0:
					sym.Sym.Value = shAddr
				case 1:
					sym.Sym.Value = shSize + shAddr
				case 2:
					sym.Sym.Value = shAddr + 0x7FF0
				}
			} else {
				sym.Sym.Shndx = 1
				sym.Section = scp
				switch cs.Seflag {
				case 0:
					sym.Sym.Value = 0
				case 1:
					sym.Sym.Value = shSize
				}
			}
			continue
		}
		if cs.Bind == elf.STBWeak && (sym.Bind == elf.STBGlobal || sym.Bind == elf.STBWeak) && sym.Sym.Shndx == 0 {
			if sym.Type == 0 {
				sym.Type = cs.Type
			}
			sym.Bind = cs.Bind
			sym.Sym.Info = uint8(((cs.Bind & 0xFF) << 4) + (cs.Type & 0xF))
		}
	}
	return nil
}

func checkUndefSymboles(f *elf.File) error {
	if f.Sections == nil {
		return nil
	}
	errCount := 0
	for _, s := range f.Sections[1:] {
		if s.Shdr.Type == elf.SHTRel {
			errCount += checkUndefSymbolesAnReloc(s)
		}
	}
	if errCount > 0 {
		return fmt.Errorf("%d undefined symbol(s)", errCount)
	}
	return nil
}

func checkUndefSymbolesAnReloc(relsect *elf.Section) int {
	undefcount := 0
	var nullSym *elf.Symbol
	if relsect.Link != nil && len(relsect.Link.Symbols) > 0 {
		nullSym = relsect.Link.Symbols[0]
	}
	for _, rel := range relsect.Relocs {
		if rel.Symbol == nil || rel.Symbol == nullSym {
			continue
		}
		if rel.Symbol.Sym.Shndx != 0 {
			shndx := rel.Symbol.Sym.Shndx
			if shndx > 0xFEFF && shndx != elf.SHNAbs && shndx != elf.SHNRaddr {
				if shndx == elf.SHNCommon {
					fmt.Fprintf(os.Stderr, "  unallocated variable `%s'\n", rel.Symbol.Name)
				} else {
					fmt.Fprintf(os.Stderr, "  `%s' unknown symbol type %x\n", rel.Symbol.Name, shndx)
				}
				undefcount++
			}
		} else if rel.Symbol.Bind != elf.STBWeak {
			fmt.Fprintf(os.Stderr, "  undefined reference to `%s'\n", rel.Symbol.Name)
			undefcount++
		}
	}
	return undefcount
}

func symbolValueUpdate(f *elf.File, tp *config.Table) {
	if f.Ehdr.Type != elf.ETRel {
		return
	}
	switch tp.Target {
	case config.TargetIOP:
		f.Ehdr.Type = elf.ETSceIopRelExec
	case config.TargetEE:
		f.Ehdr.Type = elf.ETSceEeRelExec2
	}
	scp := f.SearchSection(elf.SHTSymTab)
	if scp == nil {
		return
	}
	for _, sym := range scp.Symbols[1:] {
		if sym.Sym.Shndx != 0 && sym.Sym.Shndx <= 0xFEFF && sym.Section != nil {
			sym.Sym.Value += sym.Section.Shdr.Addr
		}
	}
}
