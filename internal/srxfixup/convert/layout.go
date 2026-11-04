package convert

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ps2build/tools/internal/srxfixup/config"
	"github.com/ps2build/tools/internal/srxfixup/elf"
)

// searchOrderSlots finds the next not-yet-consumed slot matching ordstr
// - either the special
// "@Section_header_table"/"@Program_header_data N" keywords, or a
// section-name pattern (matched against a lone section-data slot, or
// against every section inside a program-header-entry slot, which
// places that whole segment as one unit the moment any of its sections
// matches).
func searchOrderSlots(ordstr string, f *elf.File, order []elf.FileSlot) int {
	if ordstr == "@Section_header_table" {
		for i := range order {
			if order[i].Type == elf.EFSSectionHeaderTable {
				return i
			}
		}
		return -1
	}
	if strings.HasPrefix(ordstr, "@Program_header_data ") {
		n, _ := strconv.Atoi(strings.TrimPrefix(ordstr, "@Program_header_data "))
		for i := range order {
			if order[i].Type == elf.EFSProgramHeaderEntry && n < len(f.Prog) && order[i].Php == f.Prog[n] {
				return i
			}
		}
		return -1
	}
	for i := range order {
		switch order[i].Type {
		case elf.EFSProgramHeaderEntry:
			for _, s := range order[i].Php.Sections {
				if sectNameMatch(ordstr, s.Name) == 0 {
					return i
				}
			}
		case elf.EFSSectionData:
			if sectNameMatch(ordstr, order[i].Scp.Name) == 0 {
				return i
			}
		}
	}
	return -1
}

// LayoutSrxFile places every file-resident piece in the order the
// config's FileLayoutOrder demands (rather than wherever it happened to
// be before), then re-tightens offsets/alignment and writes them back
// into the real ELF structures.
func LayoutSrxFile(f *elf.File, tp *config.Table) error {
	maxSegAlign := uint32(16)
	if tp.Target == config.TargetEE {
		maxSegAlign = 0x10000
	}

	elf.ReorderSymtab(f)
	elf.RebuildSectionNameStrings(f)
	elf.RebuildSymbolNameStrings(f)

	order := elf.BuildFileOrderList(f)
	var neworder []elf.FileSlot
	neworder = append(neworder, order[0]) // ELF header
	order[0].Type = elf.EFSNone
	if f.Ehdr.Phnum != 0 && len(order) > 1 {
		neworder = append(neworder, order[1]) // program header table
		order[1].Type = elf.EFSNone
	}

	for _, pattern := range tp.FileLayoutOrder {
		for {
			idx := searchOrderSlots(pattern, f, order)
			if idx < 0 {
				break
			}
			neworder = append(neworder, order[idx])
			order[idx].Type = elf.EFSNone
		}
	}

	elf.ShrinkFileOrderList(neworder)
	elf.WritebackFileOrderList(f, neworder)

	errCount := 0
	for _, slot := range neworder {
		if slot.Type == elf.EFSProgramHeaderEntry && slot.Php.Phdr.Type == elf.PTLoad && maxSegAlign < slot.Align {
			fmt.Fprintf(os.Stderr, "Program Header Entry: unsupported align %d\n", slot.Align)
			errCount++
			for _, s := range slot.Php.Sections {
				if maxSegAlign < s.Shdr.Addralign {
					fmt.Fprintf(os.Stderr, "Section '%s' : unsupported section align %d\n", s.Name, s.Shdr.Addralign)
					errCount++
				}
			}
		}
	}
	if errCount > 0 && tp.Target == config.TargetIOP {
		fmt.Fprintln(os.Stderr, "LOADCORE limits possible alignment to 16 bytes")
	}
	if errCount > 0 {
		return fmt.Errorf("%d layout alignment error(s)", errCount)
	}
	return nil
}

// StripElf drops .mdebug and every symbol table entry with no surviving
// relocation references to it (kept-alive section symbols and anything
// still named/referenced pass through unchanged).
func StripElf(f *elf.File) {
	f.RemoveSection(elf.SHTMipsDebug)
	scp := f.SearchSection(elf.SHTSymTab)
	if scp == nil {
		return
	}
	var kept []*elf.Symbol
	kept = append(kept, scp.Symbols[0])
	for _, sym := range scp.Symbols[1:] {
		if sym.RefCount > 0 {
			kept = append(kept, sym)
		} else {
			sym.Number = -1
		}
	}
	scp.Symbols = kept
	scp.Shdr.Size = uint32(len(kept)) * scp.Shdr.Entsize
}
