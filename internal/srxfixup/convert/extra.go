package convert

import (
	"fmt"

	"github.com/ps2build/tools/internal/srxfixup/elf"
)

// ConvertRelativeBranch implements the --rb/--relative-branch flag:
// rewrites any R_MIPS_26 absolute j/jal whose target is close enough to
// fit an 18-bit PC-relative range into that smaller relative encoding,
// dropping the now-unneeded relocation entry.
func ConvertRelativeBranch(f *elf.File) {
	if f.Sections == nil {
		return
	}
	for _, s := range f.Sections[1:] {
		if s.Shdr.Type == elf.SHTRel {
			convertRelativeBranchSection(s)
		}
	}
}

func convertRelativeBranchSection(relsect *elf.Section) {
	info := relsect.Info
	var nullSym *elf.Symbol
	if relsect.Link != nil && len(relsect.Link.Symbols) > 0 {
		nullSym = relsect.Link.Symbols[0]
	}
	rmcount := 0
	for _, rp := range relsect.Relocs {
		if rp.Symbol != nil && rp.Symbol != nullSym {
			panic("Internal error: Illegal relocation entry")
		}
		if rp.Type == elf.RMipsSceAddend || rp.Type == elf.RMipsNone {
			if rp.Type == elf.RMipsNone {
				rmcount++
			}
			continue
		}
		if info.Shdr.Addr > rp.Rel.Offset || rp.Rel.Offset >= info.Shdr.Size+info.Shdr.Addr {
			panic(fmt.Sprintf("relocation offset=0x%x range out (section limit addr=0x%x-0x%x)",
				rp.Rel.Offset, info.Shdr.Addr, info.Shdr.Size+info.Shdr.Addr))
		}
		daddr := rp.Rel.Offset - info.Shdr.Addr
		if rp.Type != elf.RMips26 {
			continue
		}
		data := readU32(info.Data, daddr)
		if rp.Symbol != nil && rp.Symbol.Bind != elf.STBLocal {
			panic("R_MIPS_26 Unexcepted bind")
		}
		raddr := rp.Rel.Offset + info.Shdr.Addr
		jtarget := ((rp.Rel.Offset & 0xF0000000) | (4 * (data & 0x3FFFFFF))) - 4 - raddr
		if jtarget>>18 == 0 || jtarget>>18 == 0x3FFF {
			jaddr := uint32(uint16(jtarget >> 2))
			switch data >> 26 {
			case 2: // j
				writeU32(info.Data, daddr, jaddr|0x10000000)
				rp.Type = elf.RMipsNone
				rmcount++
			case 3: // jal
				writeU32(info.Data, daddr, jaddr|0x4110000)
				rp.Type = elf.RMipsNone
				rmcount++
			}
		}
	}
	if rmcount > 0 && rmcount < len(relsect.Relocs) {
		var kept []*elf.Reloc
		for _, r := range relsect.Relocs {
			if r.Type != elf.RMipsNone {
				kept = append(kept, r)
			}
		}
		relsect.Relocs = kept
		relsect.Shdr.Size = relsect.Shdr.Entsize * uint32(len(kept))
	}
}

// CheckZeroTextSymbols implements the --allow-zero-text guard: a
// non-export, non-entry symbol sitting at .text+0 almost always means an
// export table (exports.o) wasn't linked first, silently producing a
// module with no working export slot 0.
func CheckZeroTextSymbols(source string, f *elf.File) bool {
	if f.Sections == nil {
		return false
	}
	var textSection *elf.Section
	for _, s := range f.Sections[1:] {
		if s.Name == ".text" {
			textSection = s
			break
		}
	}
	if textSection == nil {
		return false
	}
	found := false
	for _, s := range f.Sections[1:] {
		if s.Shdr.Type != elf.SHTSymTab || s.Symbols == nil {
			continue
		}
		for j, sym := range s.Symbols[1:] {
			if sym.Name == "_start" || sym.Name == "_ftext" {
				continue
			}
			if sym.Section == textSection &&
				(sym.Type == elf.STTFunc || sym.Type == elf.STTNoType) && sym.Sym.Value == 0 {
				name := sym.Name
				if name == "" {
					name = "<unnamed>"
				}
				fmt.Printf("ERROR: %s: symbol '%s' (sym#%d) is in .text with value 0"+
					" -- if this module has an export table, put exports.o"+
					" first in IOP_OBJS and move _retonly after"+
					" DECLARE_EXPORT_TABLE in exports.tab;"+
					" otherwise use --allow-zero-text.\n", source, name, j+1)
				found = true
			}
		}
	}
	return found
}
