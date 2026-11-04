package convert

import (
	"fmt"

	"github.com/ps2build/tools/internal/srxfixup/elf"
)

// fixlocationAnRel applies a fixed base address to every relocation in
// relsect, resolving each one down to a plain absolute value. This is
// the second, address-baking pass used once a module's final load
// address is known (-t/-f), as opposed to rebuildAnRelocation's
// relative-to-the-module pass during the REL->SRX conversion itself.
func fixlocationAnRel(relsect *elf.Section, startaddr uint32) {
	info := relsect.Info
	relocs := relsect.Relocs
	i := 0
	for i < len(relocs) {
		rp := relocs[i]
		if info.Shdr.Addr > rp.Rel.Offset || rp.Rel.Offset >= info.Shdr.Size+info.Shdr.Addr {
			panic(fmt.Sprintf("relocation #%d offset=0x%x range out (section limit addr=0x%x-0x%x)",
				i, rp.Rel.Offset, info.Shdr.Addr, info.Shdr.Size+info.Shdr.Addr))
		}
		daddr := rp.Rel.Offset - info.Shdr.Addr
		switch rp.Type {
		case elf.RMipsNone:

		case elf.RMips16:
			v := startaddr + uint32(int16(readU32(info.Data, daddr)))
			if hi := uint16(v >> 16); hi != 0 && hi != 0xFFFF {
				panic("REFHALF data overflow")
			}
			writeU32(info.Data, daddr, (readU32(info.Data, daddr)&0xFFFF0000)|uint32(uint16(v)))

		case elf.RMips32:
			writeU32(info.Data, daddr, readU32(info.Data, daddr)+startaddr)

		case elf.RMips26:
			if rp.Symbol != nil && rp.Symbol.Bind != elf.STBLocal {
				panic("R_MIPS_26 Unexcepted bind")
			}
			data2 := startaddr + ((rp.Rel.Offset & 0xF0000000) | (4 * (readU32(info.Data, daddr) & 0x3FFFFFF)))
			writeU32(info.Data, daddr, (readU32(info.Data, daddr)&0xFC000000)|((16*data2)>>6))

		case elf.RMipsHi16:
			lo := findPairedLo16(relocs, i+1, rp.Symbol)
			if lo == nil {
				panic("R_MIPS_HI16 without R_MIPS_LO16")
			}
			loDaddr := lo.Rel.Offset - info.Shdr.Addr
			data4 := startaddr + uint32(int16(readU32(info.Data, loDaddr))) + (readU32(info.Data, daddr) << 16)
			writeU32(info.Data, daddr, (readU32(info.Data, daddr)&0xFFFF0000)|uint32(uint16(((data4>>15)+1)>>1)))

		case elf.RMipsLo16:
			v := startaddr + readU32(info.Data, daddr)
			writeU32(info.Data, daddr, (readU32(info.Data, daddr)&0xFFFF0000)|uint32(uint16(v)))

		case elf.RMipsGpRel16:
			panic("Unexcepted R_MIPS_GPREL16")
		case elf.RMipsLiteral:
			panic("Unexcepted R_MIPS_LITERAL")

		case elf.RMipsSceMhi16:
			if i+1 >= len(relocs) || relocs[i+1].Type != elf.RMipsSceAddend {
				panic("R_MIPSSCE_MHI16 without R_MIPSSCE_ADDEND")
			}
			addend := relocs[i+1].Rel.Offset
			data3 := uint32(uint16((((startaddr + addend) >> 15) + 1) >> 1))
			// MHI16-tagged half-words form a chain, linked by a signed
			// delta packed into each half-word's own low 16 bits as
			// (byteDelta >> 2), sign-extended from bit 15 (same encoding
			// rebuildAnRelocation's MHI16 branch writes via "step>>2").
			// Every linked half-word gets patched with the same resolved
			// high part; delta==0 marks the chain's end.
			d := daddr
			for {
				lowHalf := int16(uint16(readU32(info.Data, d)))
				delta := int32(lowHalf) << 2
				writeU32(info.Data, d, (readU32(info.Data, d)&0xFFFF0000)|data3)
				if delta == 0 {
					break
				}
				d = uint32(int64(d) + int64(delta))
			}
			i++ // consume the paired ADDEND entry too

		case elf.RMipsRel32, elf.RMipsGot16, elf.RMipsPc16, elf.RMipsCall16, elf.RMipsGpRel32,
			elf.RMipsGotHi16, elf.RMipsGotLo16, elf.RMipsCallHi16, elf.RMipsCallLo16:
			panic(fmt.Sprintf("unacceptable relocation type: 0x%x", rp.Type))
		default:
			panic(fmt.Sprintf("unknown relocation type: 0x%x", rp.Type))
		}
		i++
	}
}

// FixLocationElf bakes a fixed load address into every relocation,
// module-info struct, section, and RADDR-tagged symbol, dropping the
// now-unneeded SHT_REL sections entirely - turning a relocatable SRX
// module into a plain fixed-address ET_EXEC.
func FixLocationElf(f *elf.File, startaddr uint32) {
	if f.Sections == nil {
		return
	}
	kept := []*elf.Section{f.Sections[0]}
	for _, s := range f.Sections[1:] {
		if s.Shdr.Type == elf.SHTRel {
			fixlocationAnRel(s, startaddr)
		} else {
			kept = append(kept, s)
		}
	}
	f.Sections = kept
	f.Ehdr.Shnum = uint16(len(kept))
	f.Ehdr.Entry += startaddr

	if s := f.SearchSection(elf.SHTSceIopMod); s != nil {
		m := s.IopMod()
		if m.ModuleInfo != 0xFFFFFFFF {
			m.ModuleInfo += startaddr
		}
		m.Entry += startaddr
		m.GPValue += startaddr
	}
	if s := f.SearchSection(elf.SHTSceEeMod); s != nil {
		m := s.EeMod()
		if m.ModuleInfo != 0xFFFFFFFF {
			m.ModuleInfo += startaddr
		}
		m.Entry += startaddr
		m.GPValue += startaddr
	}
	for _, php := range f.Prog {
		if php.Phdr.Type == elf.PTLoad {
			php.Phdr.Vaddr = startaddr
			php.Phdr.Paddr = startaddr
			break
		}
	}
	for _, s := range f.Sections[1:] {
		if s.Shdr.Type == elf.SHTProgBits || s.Shdr.Type == elf.SHTNoBits {
			s.Shdr.Addr += startaddr
		}
	}
	scp := f.SearchSection(elf.SHTSymTab)
	if scp == nil {
		return
	}
	for _, sym := range scp.Symbols[1:] {
		if sym.Sym.Shndx == elf.SHNRaddr || (sym.Sym.Shndx != 0 && sym.Sym.Shndx <= 0xFEFF) {
			sym.Sym.Value += startaddr
		}
		if sym.Sym.Shndx == elf.SHNRaddr {
			sym.Sym.Shndx = 0xFFF1 // -15 as uint16
		}
	}
}
