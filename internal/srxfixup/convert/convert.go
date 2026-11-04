// Package convert turns a relocatable MIPS ELF (.o) into a PS2-loadable
// SRX/IRX/ERX relocatable module, and does the reverse fixed-location
// conversion (-f/-t).
package convert

import (
	"fmt"

	"github.com/ps2build/tools/internal/srxfixup/config"
	"github.com/ps2build/tools/internal/srxfixup/elf"
)

// ConvertRel2Srx is the main ET_REL -> SRX conversion pipeline.
func ConvertRel2Srx(f *elf.File, conf *config.Table, entrysym string, needOutput, causeIrx1 bool) error {
	saveOrgAddrs(f)

	var modinfo *elf.Section
	switch conf.Target {
	case config.TargetIOP:
		modinfo = elf.NewIopModSection()
		f.AddSection(modinfo)
	case config.TargetEE:
		modinfo = elf.NewEeModSection()
		f.AddSection(modinfo)
	default:
		return fmt.Errorf("internal error: target unknown")
	}

	removeUnuseSection(f, conf)
	defineSpecialSectionSymbols(f, conf)
	createNeedSection(f, conf)
	reorderSectionTable(f, conf)
	createPhdr(f, conf)
	if err := layoutSrxMemory(f, conf); err != nil {
		return err
	}
	modifyEeMod(f, modinfo)
	if err := createReservedSymbols(f, conf); err != nil {
		return err
	}
	if err := checkUndefSymboles(f); err != nil {
		return err
	}
	symbolValueUpdate(f, conf)
	global := conf.Segment("GLOBALDATA")
	if global == nil {
		return fmt.Errorf("internal error: GLOBALDATA segment missing")
	}
	rebuildRelocation(f, global.Addr+0x7FF0, conf.Target)
	if err := checkIrx12(f, causeIrx1); err != nil {
		return err
	}
	if err := setupStartEntry(f, entrysym, modinfo, needOutput); err != nil {
		return err
	}
	moduleInfoSymbol := "Module"
	if syp := f.SearchGlobalSymbol("_irx_id"); syp.IsDefined() {
		moduleInfoSymbol = "_irx_id"
	}
	setupModuleInfo(f, modinfo, moduleInfoSymbol)

	return LayoutSrxFile(f, conf)
}

func setupStartEntry(f *elf.File, entrysym string, modinfo *elf.Section, needOutput bool) error {
	var syp *elf.Symbol
	if entrysym != "" {
		syp = f.SearchGlobalSymbol(entrysym)
		if !syp.IsDefined() {
			return fmt.Errorf("cannot find entry symbol %s", entrysym)
		}
		f.Ehdr.Entry = syp.Value(f)
	} else {
		syp = f.SearchGlobalSymbol("start")
		if syp == nil {
			syp = f.SearchGlobalSymbol("_start")
		}
		if !syp.IsDefined() {
			if modinfo.Shdr.Type == elf.SHTSceEeMod {
				f.Ehdr.Entry = 0xFFFFFFFF
			} else if needOutput {
				fmt.Println("warning: Cannot find entry symbol `start' and `_start'")
			}
		} else {
			f.Ehdr.Entry = syp.Value(f)
		}
	}
	switch modinfo.Shdr.Type {
	case elf.SHTSceIopMod:
		m := modinfo.IopMod()
		m.Entry = f.Ehdr.Entry
	case elf.SHTSceEeMod:
		m := modinfo.EeMod()
		m.Entry = f.Ehdr.Entry
	}
	return nil
}

func saveOrgAddrs(f *elf.File) {
	reginfo := f.SearchSection(elf.SHTMipsRegInfo)
	var gpValue uint32
	if reginfo != nil && len(reginfo.Data) >= 20 {
		// Elf32_RegInfo: gprmask(4) cprmask[4](16) gp_value(4) - gp_value
		// is the last 4 bytes.
		off := len(reginfo.Data) - 4
		gpValue = uint32(reginfo.Data[off]) | uint32(reginfo.Data[off+1])<<8 | uint32(reginfo.Data[off+2])<<16 | uint32(reginfo.Data[off+3])<<24
	}
	for _, s := range f.Sections[1:] {
		s.OrgAddr = s.Shdr.Addr
		if reginfo != nil {
			s.OrgGPValue = gpValue
		}
	}
}

func modifyEeMod(f *elf.File, eemod *elf.Section) {
	if eemod.Shdr.Type != elf.SHTSceEeMod {
		return
	}
	m := eemod.EeMod()
	if s := f.SearchSectionByName(".erx.lib"); s != nil {
		m.ErxLibAddr = s.Shdr.Addr
		m.ErxLibSize = s.Shdr.Size
	} else {
		m.ErxLibAddr = 0xFFFFFFFF
		m.ErxLibSize = 0
	}
	if s := f.SearchSectionByName(".erx.stub"); s != nil {
		m.ErxStubAddr = s.Shdr.Addr
		m.ErxStubSize = s.Shdr.Size
	} else {
		m.ErxStubAddr = 0xFFFFFFFF
		m.ErxStubSize = 0
	}
}

func addReservedSymbolTable(tp *config.Table, name string, bind, typ int, segment *config.SegConf, sectname string, shindex, seflag int) {
	tp.CreateSymbols = append(tp.CreateSymbols, &config.CreateSymbolConf{
		Name: name, Bind: bind, Type: typ, Segment: segment, SectName: sectname, Shindex: shindex, Seflag: seflag,
	})
}

const bosStr = "_begin_of_section_"
const eosStr = "_end_of_section_"

func defineSpecialSectionSymbols(f *elf.File, tp *config.Table) {
	scp := f.SearchSection(elf.SHTSymTab)
	if scp == nil {
		return
	}
	for _, sym := range scp.Symbols[1:] {
		if sym.Bind != elf.STBGlobal || sym.Sym.Shndx != 0 {
			continue
		}
		if len(sym.Name) > len(bosStr) && sym.Name[:len(bosStr)] == bosStr {
			addReservedSymbolTable(tp, sym.Name, elf.STBWeak, elf.STTObject, nil, "."+sym.Name[len(bosStr):], 0, 0)
		}
		if len(sym.Name) > len(eosStr) && sym.Name[:len(eosStr)] == eosStr {
			addReservedSymbolTable(tp, sym.Name, elf.STBWeak, elf.STTObject, nil, "."+sym.Name[len(eosStr):], elf.SHNRaddr, 1)
		}
	}
}

func createNeedSection(f *elf.File, tp *config.Table) {
	scp := f.SearchSection(elf.SHTSymTab)
	if scp == nil {
		return
	}
	for _, sym := range scp.Symbols[1:] {
		if sym.Sym.Shndx != 0 {
			continue
		}
		csym := isReserveSymbol(tp, sym.Name)
		if csym == nil {
			continue
		}
		if csym.Segment != nil {
			addscp := csym.Segment.EmptySection
			if addscp != nil && f.SearchSectionByName(addscp.Name) == nil {
				f.AddSection(addscp)
			}
			continue
		}
		if csym.SectName != "" && f.SearchSectionByName(csym.SectName) == nil {
			for _, odr := range tp.Sections {
				if sectNameMatch(odr.Pattern, csym.SectName) == 0 && odr.SectType != 0 && odr.SectFlag != 0 {
					ns := &elf.Section{Name: csym.SectName}
					ns.Shdr.Type = odr.SectType
					ns.Shdr.Flags = odr.SectFlag
					ns.Shdr.Addralign = 4
					f.AddSection(ns)
					break
				}
			}
		}
	}
}

func isReserveSymbol(tp *config.Table, name string) *config.CreateSymbolConf {
	if name == "" {
		return nil
	}
	for _, cs := range tp.CreateSymbols {
		if cs.Name == name {
			return cs
		}
	}
	return nil
}

func reorderSectionTable(f *elf.File, tp *config.Table) {
	old := append([]*elf.Section(nil), f.Sections...)
	newSections := make([]*elf.Section, 1, len(old))
	newSections[0] = old[0]
	used := make([]bool, len(old))
	used[0] = true
	for _, pattern := range tp.SectionTableOrder {
		for i := 1; i < len(old); i++ {
			if used[i] {
				continue
			}
			if sectNameMatch(pattern, old[i].Name) == 0 {
				newSections = append(newSections, old[i])
				used[i] = true
			}
		}
	}
	f.Sections = newSections
	f.Ehdr.Shnum = uint16(len(newSections))
	elf.ReorderSymtab(f)
}

func createPhdr(f *elf.File, tp *config.Table) {
	f.Prog = nil
	for _, ph := range tp.ProgramHeaderOrder {
		php := &elf.ProgHeader{}
		switch ph.Type {
		case config.PHTypeMod:
			php.Phdr.Flags = elf.PFR
			php.Phdr.Align = 4
			switch ph.SectionName {
			case ".iopmod":
				php.Phdr.Type = elf.PTSceIopMod
				php.Phdr.Filesz = elf.IopModHeaderSize()
				if s := f.SearchSection(elf.SHTSceIopMod); s != nil {
					php.Sections = []*elf.Section{s}
				}
			case ".eemod":
				php.Phdr.Type = elf.PTSceEeMod
				php.Phdr.Filesz = elf.EeModHeaderSize()
				if s := f.SearchSection(elf.SHTSceEeMod); s != nil {
					php.Sections = []*elf.Section{s}
				}
			default:
				fmt.Printf("Unsuport section '%s' for program header\n", ph.SectionName)
			}
		case config.PHTypeText:
			php.Phdr.Type = elf.PTLoad
			php.Phdr.Flags = elf.PFX | elf.PFW | elf.PFR
			php.Phdr.Align = 16
		}
		f.Prog = append(f.Prog, php)
	}
	f.Ehdr.Phentsize = 32
	f.Ehdr.Phnum = uint16(len(f.Prog))
}

func checkIrx12(f *elf.File, causeIrx1 bool) error {
	if f.Ehdr.Type != elf.ETSceIopRelExec {
		return nil
	}
	for _, s := range f.Sections[1:] {
		if s.Shdr.Type == elf.SHTRel && relocationIsVersion2(s) {
			if causeIrx1 {
				return fmt.Errorf("R_MIPS_LO16 without R_MIPS_HI16")
			}
			f.Ehdr.Type = elf.ETSceIopRelExec2
		}
	}
	return nil
}

func setupModuleInfo(f *elf.File, modsect *elf.Section, modulesymbol string) {
	syp := f.SearchGlobalSymbol(modulesymbol)
	if !syp.IsDefined() {
		return
	}
	modiaddr := syp.Value(f)
	modidata := f.GetSectionData(modiaddr)
	if modidata == nil || len(modidata) < 4 {
		return
	}
	nameAddr := u32le(modidata)
	nameData := f.GetSectionData(nameAddr)
	name := cstrBytes(nameData)

	switch modsect.Shdr.Type {
	case elf.SHTSceIopMod:
		m := modsect.IopMod()
		m.ModuleInfo = modiaddr
		if len(modidata) >= 6 {
			m.ModuleVersion = uint16(modidata[4]) | uint16(modidata[5])<<8
		}
		m.ModuleName = ""
		modsect.SetIopMod(m)
	case elf.SHTSceEeMod:
		m := modsect.EeMod()
		m.ModuleInfo = modiaddr
		if len(modidata) >= 6 {
			m.ModuleVersion = uint16(modidata[4]) | uint16(modidata[5])<<8
		}
		m.ModuleName = name
		modsect.SetEeMod(m)
	}
	for _, php := range f.Prog {
		if len(php.Sections) > 0 && php.Sections[0] == modsect {
			php.Phdr.Filesz = modsect.Shdr.Size
		}
	}
}

func u32le(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

func cstrBytes(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}
