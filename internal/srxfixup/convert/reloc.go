package convert

import (
	"fmt"

	"github.com/ps2build/tools/internal/srxfixup/config"
	"github.com/ps2build/tools/internal/srxfixup/elf"
)

// readU32/writeU32 read/write a little-endian uint32 at a byte offset
// within a section's data. A relocation's r_offset is only ever
// out-of-range here for a compressed debug section (sh_size there is
// the on-disk compressed byte count, but the compiler computed r_offset
// against the section's real, uncompressed layout, which this tool never
// reconstructs): a debug-only discrepancy that doesn't affect a module's
// real loaded behavior, so a short/absent read is treated as 0 and an
// out-of-range write is dropped rather than crashing.
func readU32(data []byte, off uint32) uint32 {
	if int64(off)+4 > int64(len(data)) {
		return 0
	}
	return uint32(data[off]) | uint32(data[off+1])<<8 | uint32(data[off+2])<<16 | uint32(data[off+3])<<24
}

func writeU32(data []byte, off uint32, v uint32) {
	if int64(off)+4 > int64(len(data)) {
		return
	}
	data[off] = byte(v)
	data[off+1] = byte(v >> 8)
	data[off+2] = byte(v >> 16)
	data[off+3] = byte(v >> 24)
}

// findPairedLo16 searches forward from start for the R_MIPS_LO16 entry
// that pairs with a HI16 by real matching symbol, not just table
// adjacency - a HI16/LO16 pair need not be table-adjacent.
func findPairedLo16(relocs []*elf.Reloc, start int, symptr *elf.Symbol) *elf.Reloc {
	for k := start; k < len(relocs); k++ {
		if relocs[k].Type == elf.RMipsLo16 && (symptr == nil || relocs[k].Symbol == symptr) {
			return relocs[k]
		}
	}
	return nil
}

// relocationIsVersion2 reports whether this section has any relocation
// IRX1's on-disk format can't represent: a bare LO16, an already-built
// MHI16/ADDEND pair, or a HI16 whose pair isn't its immediate table
// neighbor.
func relocationIsVersion2(relsect *elf.Section) bool {
	relocs := relsect.Relocs
	for i := 0; i < len(relocs); i++ {
		switch relocs[i].Type {
		case elf.RMipsLo16, elf.RMipsSceMhi16, elf.RMipsSceAddend:
			return true
		case elf.RMipsHi16:
			if i+1 >= len(relocs) || relocs[i+1].Type != elf.RMipsLo16 ||
				(relocs[i].Symbol != nil && relocs[i+1].Symbol != relocs[i].Symbol) {
				return true
			}
			i++
		}
	}
	return false
}

func rebuildRelocation(f *elf.File, gpvalue uint32, target int) {
	for _, s := range f.Sections[1:] {
		if s.Shdr.Type == elf.SHTRel {
			rebuildAnRelocation(s, gpvalue, target)
		}
	}
}

func rebuildAnRelocation(relsect *elf.Section, gpvalue uint32, target int) {
	info := relsect.Info
	relocs := relsect.Relocs
	var nullSym *elf.Symbol
	if relsect.Link != nil && len(relsect.Link.Symbols) > 0 {
		nullSym = relsect.Link.Symbols[0]
	}
	rmflag := false

	i := 0
	for i < len(relocs) {
		rp := relocs[i]
		if info.Shdr.Size <= rp.Rel.Offset {
			panic(fmt.Sprintf("relocation #%d offset=0x%x overflow (section size=0x%x)", i, rp.Rel.Offset, info.Shdr.Size))
		}
		next := 1
		daddr := rp.Rel.Offset
		var symvalue uint32
		if rp.Symbol != nil && rp.Symbol.Sym.Shndx != 0 {
			symvalue = rp.Symbol.Sym.Value
		}
		// v4: true when the symbol is already fully resolved to a
		// section-local (or SHN_RADDR) address, so the relocation can be
		// folded away entirely instead of surviving into the output.
		v4 := rp.Symbol == nil ||
			(rp.Symbol.Sym.Shndx != 0 && rp.Symbol.Sym.Shndx <= 0xFEFF) ||
			rp.Symbol.Sym.Shndx == elf.SHNRaddr

		switch rp.Type {
		case elf.RMipsNone:
			rmflag = true

		case elf.RMips16:
			v := symvalue + uint32(int16(readU32(info.Data, daddr)))
			if hi := uint16(v >> 16); hi != 0 && hi != 0xFFFF {
				panic("REFHALF data overflow")
			}
			writeU32(info.Data, daddr, (readU32(info.Data, daddr)&0xFFFF0000)|uint32(uint16(v)))
			if !v4 {
				rp.Type = elf.RMipsNone
				rmflag = true
			}

		case elf.RMips32:
			writeU32(info.Data, daddr, readU32(info.Data, daddr)+symvalue)
			if !v4 {
				rp.Type = elf.RMipsNone
				rmflag = true
			}

		case elf.RMips26:
			data2 := readU32(info.Data, daddr)
			var data33 uint32
			if rp.Symbol != nil && rp.Symbol.Bind != elf.STBLocal {
				data33 = data2 << 6 >> 4
			} else {
				data33 = ((info.Shdr.Addr + rp.Rel.Offset) & 0xF0000000) | (4 * (data2 & 0x3FFFFFF))
			}
			writeU32(info.Data, daddr, (data2&0xFC000000)|((16*(symvalue+data33))>>6))
			if !v4 {
				rp.Type = elf.RMipsNone
				rmflag = true
			}

		case elf.RMipsHi16:
			datah := readU32(info.Data, daddr) << 16
			for i+next < len(relocs) && relocs[i+next].Type == elf.RMipsHi16 {
				if rp.Symbol != nil && relocs[i+next].Symbol != rp.Symbol {
					panic("R_MIPS_HI16 without R_MIPS_LO16")
				}
				if info.Shdr.Size <= relocs[i+next].Rel.Offset {
					panic(fmt.Sprintf("relocation #%d offset overflow", i+next))
				}
				if datah != readU32(info.Data, relocs[i+next].Rel.Offset)<<16 {
					panic("R_MIPS_HI16s not same offsets")
				}
				next++
			}
			lo := findPairedLo16(relocs, i+next, rp.Symbol)
			if lo == nil {
				panic("R_MIPS_HI16 without R_MIPS_LO16")
			}
			data321 := symvalue + uint32(int16(readU32(info.Data, lo.Rel.Offset))) + datah

			switch {
			case next == 1:
				writeU32(info.Data, daddr, (readU32(info.Data, daddr)&0xFFFF0000)|uint32(uint16(((data321>>15)+1)>>1)))
				if !v4 {
					rp.Type = elf.RMipsNone
					rmflag = true
				}
			case v4:
				for j := 0; j < next; j++ {
					d2 := relocs[i+j].Rel.Offset
					writeU32(info.Data, d2, readU32(info.Data, d2)&0xFFFF0000)
					if j < next-1 {
						step := relocs[i+j+1].Rel.Offset - relocs[i+j].Rel.Offset
						if step>>18 != 0 && step>>18 != 0x3FFF {
							panic("R_MIPS_HI16s too long distance")
						}
						writeU32(info.Data, d2, readU32(info.Data, d2)|uint32(uint16(step>>2)))
					}
					relocs[i+j].Type = elf.RMipsNone
					if relocs[i+j].Symbol != nil {
						relocs[i+j].Symbol.RefCount--
						relocs[i+j].Symbol = nullSym
					}
				}
				rp.Type = elf.RMipsSceMhi16
				rp.Rel.Offset += info.Shdr.Addr
				relocs[i+1].Type = elf.RMipsSceAddend
				relocs[i+1].Rel.Offset = data321
				rmflag = true
				i += next
				next = 0
			default:
				data322 := uint32(uint16(((data321 >> 15) + 1) >> 1))
				for j := 0; j < next; j++ {
					d3 := relocs[i+j].Rel.Offset
					writeU32(info.Data, d3, (readU32(info.Data, d3)&0xFFFF0000)|data322)
					relocs[i+j].Type = elf.RMipsNone
				}
				rmflag = true
			}

		case elf.RMipsLo16:
			v := symvalue + readU32(info.Data, daddr)
			writeU32(info.Data, daddr, (readU32(info.Data, daddr)&0xFFFF0000)|uint32(uint16(v)))
			if !v4 {
				rp.Type = elf.RMipsNone
				rmflag = true
			}

		case elf.RMipsGpRel16:
			data5 := uint32(int16(readU32(info.Data, daddr)))
			if rp.Symbol == nil {
				panic("R_MIPS_GPREL16 no symtab")
			}
			switch {
			case rp.Symbol.Type == elf.STTSection:
				sec := rp.Symbol.Section
				data5 += sec.OrgGPValue + symvalue - sec.OrgAddr - gpvalue
			case rp.Symbol.Bind == elf.STBGlobal || (rp.Symbol.Bind == elf.STBWeak && rp.Symbol.Sym.Shndx != 0):
				data5 += symvalue - gpvalue
			case rp.Symbol.Bind != elf.STBWeak || rp.Symbol.Sym.Shndx != 0:
				panic("R_MIPS_GPREL16 unknown case abort")
			}
			if hi := uint16(data5 >> 16); hi != 0 && hi != 0xFFFF {
				panic("R_MIPS_GPREL16 data overflow")
			}
			writeU32(info.Data, daddr, (readU32(info.Data, daddr)&0xFFFF0000)|uint32(uint16(data5)))
			rp.Type = elf.RMipsNone
			rmflag = true

		case elf.RMipsLiteral:
			if rp.Symbol == nil || rp.Symbol.Type != elf.STTSection {
				panic("R_MIPS_LITERAL unknown case abort")
			}
			sec := rp.Symbol.Section
			data6 := sec.OrgGPValue + symvalue - sec.OrgAddr - gpvalue + uint32(int16(readU32(info.Data, daddr)))
			if hi := uint16(data6 >> 16); hi != 0 && hi != 0xFFFF {
				panic("R_MIPS_LITERAL data overflow")
			}
			writeU32(info.Data, daddr, (readU32(info.Data, daddr)&0xFFFF0000)|uint32(uint16(data6)))
			rp.Type = elf.RMipsNone
			rmflag = true

		case elf.RMipsDvp27S4:
			if target != config.TargetEE {
				panic("R_MIPS_DVP_27_S4 can use only for EE")
			}
			data7 := symvalue + (readU32(info.Data, daddr) & 0x7FFFFFF0)
			writeU32(info.Data, daddr, (readU32(info.Data, daddr)&0x8000000F)|(data7&0x7FFFFFF0))
			if !v4 {
				rp.Type = elf.RMipsNone
				rmflag = true
			}

		case elf.RMipsRel32, elf.RMipsGot16, elf.RMipsPc16, elf.RMipsCall16, elf.RMipsGpRel32,
			elf.RMipsGotHi16, elf.RMipsGotLo16, elf.RMipsCallHi16, elf.RMipsCallLo16:
			panic(fmt.Sprintf("unacceptable relocation type: 0x%x", rp.Type))

		default:
			panic(fmt.Sprintf("unknown relocation type: 0x%x", rp.Type))
		}

		for ; next > 0; next-- {
			relocs[i].Rel.Offset += info.Shdr.Addr
			if relocs[i].Symbol != nil {
				relocs[i].Symbol.RefCount--
				relocs[i].Symbol = nullSym
			}
			i++
		}
	}

	if rmflag {
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
