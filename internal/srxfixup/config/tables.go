package config

import "github.com/ps2build/tools/internal/srxfixup/elf"

var eeSegments = []*SegConf{
	{Name: "TEXT", BitID: 0x1, SectNamePatterns: []string{".init", ".text", ".gnu.linkonce.t*", ".fini", ".vutext"}, EmptySection: makeEmptySection(".text", elf.SHTProgBits, elf.SHFAlloc|elf.SHFExecInstr)},
	{Name: "DATA", BitID: 0x2, SectNamePatterns: []string{".ctors", ".dtors", ".eh_frame", ".gcc_except_table", ".erx.lib", ".erx.stub", ".rodata", ".rodata1", ".gnu.linkonce.r*", ".data", ".data1", ".gnu.linkonce.d*", ".vudata", ".sdata", ".lit8", ".lit4", ".gnu.linkonce.s*"}, EmptySection: makeEmptySection(".data", elf.SHTProgBits, elf.SHFAlloc|elf.SHFWrite)},
	{Name: "BSS", BitID: 0x4, SectNamePatterns: []string{".sbss", ".bss", ".vubss"}},
	{Name: "GLOBALDATA", BitID: 0x8, SectNamePatterns: []string{".sdata", ".lit8", ".lit4", ".gnu.linkonce.s*", ".sbss"}},
	{Name: "AFTER_SHT", BitID: 0x0, SectNamePatterns: []string{".rel.init", ".rel.text", ".rel.gnu.linkonce.t*", ".rel.fini", ".rel.vutext", ".rel.ctors", ".rel.dtors", ".rel.eh_frame", ".rel.gcc_except_table", ".rel.erx.lib", ".rel.erx.stub", ".rel.rodata", ".rel.rodata1", ".rel.gnu.linkonce.r*", ".rel.data", ".rel.data1", ".rel.gnu.linkonce.d*", ".rel.vudata", ".rel.sdata", ".rel.gnu.linkonce.s*"}},
}

// EE is the section-layout config for EE (Emotion Engine) targets -
// produces a .eemod-headed SRX/ERX module.
var EE = &Table{
	Target:   TargetEE,
	Segments: eeSegments,
	CreateSymbols: []*CreateSymbolConf{
		{Name: "_ftext", Bind: elf.STBGlobal, Type: elf.STTObject, Segment: eeSegments[0], Shindex: 0, Seflag: 0},
		{Name: "etext", Bind: elf.STBGlobal, Type: elf.STTObject, Segment: eeSegments[0], Shindex: elf.SHNRaddr, Seflag: 1},
		{Name: "_etext", Bind: elf.STBGlobal, Type: elf.STTObject, Segment: eeSegments[0], Shindex: elf.SHNRaddr, Seflag: 1},
		{Name: "_fdata", Bind: elf.STBGlobal, Type: elf.STTObject, Segment: eeSegments[1], Shindex: 0, Seflag: 0},
		{Name: "edata", Bind: elf.STBGlobal, Type: elf.STTObject, Segment: eeSegments[1], Shindex: elf.SHNRaddr, Seflag: 1},
		{Name: "_edata", Bind: elf.STBGlobal, Type: elf.STTObject, Segment: eeSegments[1], Shindex: elf.SHNRaddr, Seflag: 1},
		{Name: "_fbss", Bind: elf.STBGlobal, Type: elf.STTObject, Segment: eeSegments[2], Shindex: elf.SHNRaddr, Seflag: 0},
		{Name: "end", Bind: elf.STBGlobal, Type: elf.STTObject, Segment: eeSegments[2], Shindex: elf.SHNRaddr, Seflag: 1},
		{Name: "_end", Bind: elf.STBGlobal, Type: elf.STTObject, Segment: eeSegments[2], Shindex: elf.SHNRaddr, Seflag: 1},
		{Name: "_gp", Bind: elf.STBGlobal, Type: elf.STTObject, Segment: eeSegments[3], Shindex: elf.SHNRaddr, Seflag: 2},
	},
	ProgramHeaderOrder: []*PHeaderInfo{
		{Type: PHTypeMod, SectionName: ".eemod"},
		{Type: PHTypeText, SegmentNames: []string{"TEXT", "DATA", "BSS"}},
	},
	Sections: []*SectConf{
		{Pattern: ".init", Flag: 0x1},
		{Pattern: ".text", Flag: 0x1, SectType: elf.SHTProgBits, SectFlag: elf.SHFAlloc | elf.SHFExecInstr},
		{Pattern: ".gnu.linkonce.t*", Flag: 0x1},
		{Pattern: ".fini", Flag: 0x1},
		{Pattern: ".vutext", Flag: 0x1},
		{Pattern: ".ctors", Flag: 0x2},
		{Pattern: ".dtors", Flag: 0x2},
		{Pattern: ".eh_frame", Flag: 0x2},
		{Pattern: ".gcc_except_table", Flag: 0x2},
		{Pattern: ".erx.lib", Flag: 0x2},
		{Pattern: ".erx.stub", Flag: 0x2},
		{Pattern: ".rodata", Flag: 0x2},
		{Pattern: ".rodata1", Flag: 0x2},
		{Pattern: ".gnu.linkonce.r*", Flag: 0x2},
		{Pattern: ".data", Flag: 0x2, SectType: elf.SHTProgBits, SectFlag: elf.SHFAlloc | elf.SHFWrite},
		{Pattern: ".data1", Flag: 0x2},
		{Pattern: ".gnu.linkonce.d*", Flag: 0x2},
		{Pattern: ".vudata", Flag: 0x2},
		{Pattern: ".sdata", Flag: 0xa},
		{Pattern: ".lit8", Flag: 0xa},
		{Pattern: ".lit4", Flag: 0xa},
		{Pattern: ".gnu.linkonce.s*", Flag: 0xa},
		{Pattern: ".sbss", Flag: 0xc},
		{Pattern: ".bss", Flag: 0x4},
		{Pattern: ".vubss", Flag: 0x4},
	},
	RemoveSectionList: []string{".reginfo", ".mdebug.*", ".DVP.*", ".rel.DVP.*", ".jcr", ".rel.jcr", ".rela.*"},
	SectionTableOrder: []string{".reginfo", ".eemod", ".init", ".rel.init", ".text", ".rel.text", ".gnu.linkonce.t*", ".rel.gnu.linkonce.t*", ".fini", ".rel.fini", ".vutext", ".rel.vutext", ".ctors", ".rel.ctors", ".dtors", ".rel.dtors", ".eh_frame", ".rel.eh_frame", ".gcc_except_table", ".rel.gcc_except_table", ".erx.lib", ".rel.erx.lib", ".erx.stub", ".rel.erx.stub", ".rodata", ".rel.rodata", ".rodata1", ".rel.rodata1", ".gnu.linkonce.r*", ".rel.gnu.linkonce.r*", ".data", ".rel.data", ".data1", ".rel.data1", ".gnu.linkonce.d*", ".rel.gnu.linkonce.d*", ".vudata", ".rel.vudata", ".sdata", ".rel.sdata", ".lit8", ".lit4", ".gnu.linkonce.s*", ".rel.gnu.linkonce.s*", ".sbss", ".bss", ".vubss", ".mdebug", ".shstrtab", ".mdebug.*", ".DVP.*", ".rel.DVP.*", ".jcr", ".rel.jcr", ".rela.*", ".symtab", ".strtab", "*"},
	FileLayoutOrder:   []string{".eemod", "@Program_header_data 1", ".mdebug", ".shstrtab", "@Section_header_table", ".rel.init", ".rel.text", ".rel.gnu.linkonce.t*", ".rel.fini", ".rel.vutext", ".rel.ctors", ".rel.dtors", ".rel.eh_frame", ".rel.gcc_except_table", ".rel.erx.lib", ".rel.erx.stub", ".rel.rodata", ".rel.rodata1", ".rel.gnu.linkonce.r*", ".rel.data", ".rel.data1", ".rel.gnu.linkonce.d*", ".rel.vudata", ".rel.sdata", ".rel.gnu.linkonce.s*", ".symtab", ".strtab", "*"},
}

var iopSegments = []*SegConf{
	{Name: "TEXT", BitID: 0x1, SectNamePatterns: []string{".init", ".text", ".fini"}, EmptySection: makeEmptySection(".text", elf.SHTProgBits, elf.SHFAlloc|elf.SHFExecInstr)},
	{Name: "DATA", BitID: 0x2, SectNamePatterns: []string{".rodata", ".rodata1", ".data", ".data1", ".ctors", ".dtors", ".eh_frame", ".sdata", ".lit8", ".lit4"}, EmptySection: makeEmptySection(".data", elf.SHTProgBits, elf.SHFAlloc|elf.SHFWrite)},
	{Name: "BSS", BitID: 0x4, SectNamePatterns: []string{".sbss", ".bss"}},
	{Name: "GLOBALDATA", BitID: 0x8, SectNamePatterns: []string{".sdata", ".lit8", ".lit4", ".sbss"}},
	{Name: "AFTER_SHT", BitID: 0x0, SectNamePatterns: []string{".rel.init", ".rel.text", ".rel.fini", ".rel.rodata", ".rel.rodata1", ".rel.data", ".rel.data1", ".rel.ctors", ".rel.dtors", ".rel.eh_frame", ".rel.sdata"}},
}

// IOP is the section-layout config for IOP targets - produces an
// .iopmod-headed SRX/IRX module.
var IOP = &Table{
	Target:   TargetIOP,
	Segments: iopSegments,
	CreateSymbols: []*CreateSymbolConf{
		{Name: "_ftext", Bind: elf.STBGlobal, Type: elf.STTObject, Segment: iopSegments[0], Shindex: 0, Seflag: 0},
		{Name: "etext", Bind: elf.STBGlobal, Type: elf.STTObject, Segment: iopSegments[0], Shindex: elf.SHNRaddr, Seflag: 1},
		{Name: "_etext", Bind: elf.STBGlobal, Type: elf.STTObject, Segment: iopSegments[0], Shindex: elf.SHNRaddr, Seflag: 1},
		{Name: "_fdata", Bind: elf.STBGlobal, Type: elf.STTObject, Segment: iopSegments[1], Shindex: 0, Seflag: 0},
		{Name: "edata", Bind: elf.STBGlobal, Type: elf.STTObject, Segment: iopSegments[1], Shindex: elf.SHNRaddr, Seflag: 1},
		{Name: "_edata", Bind: elf.STBGlobal, Type: elf.STTObject, Segment: iopSegments[1], Shindex: elf.SHNRaddr, Seflag: 1},
		{Name: "_fbss", Bind: elf.STBGlobal, Type: elf.STTObject, Segment: iopSegments[2], Shindex: elf.SHNRaddr, Seflag: 0},
		{Name: "end", Bind: elf.STBGlobal, Type: elf.STTObject, Segment: iopSegments[2], Shindex: elf.SHNRaddr, Seflag: 1},
		{Name: "_end", Bind: elf.STBGlobal, Type: elf.STTObject, Segment: iopSegments[2], Shindex: elf.SHNRaddr, Seflag: 1},
		{Name: "_gp", Bind: elf.STBGlobal, Type: elf.STTObject, Segment: iopSegments[3], Shindex: elf.SHNRaddr, Seflag: 2},
	},
	ProgramHeaderOrder: []*PHeaderInfo{
		{Type: PHTypeMod, SectionName: ".iopmod"},
		{Type: PHTypeText, SegmentNames: []string{"TEXT", "DATA", "BSS"}},
	},
	Sections: []*SectConf{
		{Pattern: ".init", Flag: 0x1},
		{Pattern: ".text", Flag: 0x1, SectType: elf.SHTProgBits, SectFlag: elf.SHFAlloc | elf.SHFExecInstr},
		{Pattern: ".fini", Flag: 0x1},
		{Pattern: ".rodata", Flag: 0x2},
		{Pattern: ".rodata1", Flag: 0x2},
		{Pattern: ".data", Flag: 0x2, SectType: elf.SHTProgBits, SectFlag: elf.SHFAlloc | elf.SHFWrite},
		{Pattern: ".data1", Flag: 0x2},
		{Pattern: ".ctors", Flag: 0x2},
		{Pattern: ".dtors", Flag: 0x2},
		{Pattern: ".eh_frame", Flag: 0x2},
		{Pattern: ".sdata", Flag: 0xa},
		{Pattern: ".lit8", Flag: 0xa},
		{Pattern: ".lit4", Flag: 0xa},
		{Pattern: ".sbss", Flag: 0xc},
		{Pattern: ".bss", Flag: 0x4},
	},
	RemoveSectionList: []string{".reginfo"},
	SectionTableOrder: []string{".reginfo", ".iopmod", ".init", ".rel.init", ".text", ".rel.text", ".fini", ".rel.fini", ".rodata", ".rel.rodata", ".rodata1", ".rel.rodata1", ".data", ".rel.data", ".data1", ".rel.data1", ".ctors", ".rel.ctors", ".dtors", ".rel.dtors", ".eh_frame", ".rel.eh_frame", ".sdata", ".rel.sdata", ".lit8", ".lit4", ".sbss", ".bss", ".mdebug", ".shstrtab", ".symtab", ".strtab", "*"},
	FileLayoutOrder:   []string{".iopmod", "@Program_header_data 1", ".mdebug", ".shstrtab", "@Section_header_table", ".rel.init", ".rel.text", ".rel.fini", ".rel.rodata", ".rel.rodata1", ".rel.data", ".rel.data1", ".rel.ctors", ".rel.dtors", ".rel.eh_frame", ".rel.sdata", ".symtab", ".strtab", "*"},
}
