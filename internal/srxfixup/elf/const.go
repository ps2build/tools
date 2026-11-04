package elf

// ELF e_type values, including the SCE (Sony Computer Entertainment)
// relocatable-module extensions srxfixup itself produces - not part of
// the standard ELF spec.
const (
	ETNone           = 0
	ETRel            = 1
	ETExec           = 2
	ETDyn            = 3
	ETCore           = 4
	ETSceIopRelExec  = 0xFF80
	ETSceIopRelExec2 = 0xFF81
	ETSceEeRelExec   = 0xFF90
	ETSceEeRelExec2  = 0xFF91
)

const (
	EMMips = 8
)

const (
	ELFClass32  = 1
	ELFData2LSB = 1
	EVCurrent   = 1
)

// e_flags (EF_MIPS_*)
const (
	EFMipsMach     = 0x00FF0000
	EFMipsMach5900 = 0x00920000
	EFMipsArch3    = 0x20000000
	EFMipsArch     = 0xF0000000
)

// Program header p_type, including SCE module-info segment types.
const (
	PTNull        = 0
	PTLoad        = 1
	PTMipsRegInfo = 0x70000000
	PTSceIopMod   = 0x70000080
	PTSceEeMod    = 0x70000090
)

const (
	PFX = 1
	PFW = 2
	PFR = 4
)

// Section header sh_type, including MIPS and SCE extensions.
const (
	SHTNull        = 0
	SHTProgBits    = 1
	SHTSymTab      = 2
	SHTStrTab      = 3
	SHTRela        = 4
	SHTHash        = 5
	SHTDynamic     = 6
	SHTNote        = 7
	SHTNoBits      = 8
	SHTRel         = 9
	SHTShLib       = 10
	SHTDynSym      = 11
	SHTMipsRegInfo = 0x70000006
	SHTMipsDebug   = 0x70000005
	SHTSceIopMod   = 0x70000080
	SHTSceEeMod    = 0x70000090
)

const (
	SHFWrite     = 1
	SHFAlloc     = 2
	SHFExecInstr = 4
)

// MIPS relocation types, including the SCE MHI16/ADDEND extensions this
// tool's own srx-format conversion introduces.
const (
	RMipsNone      = 0
	RMips16        = 1
	RMips32        = 2
	RMipsRel32     = 3
	RMips26        = 4
	RMipsHi16      = 5
	RMipsLo16      = 6
	RMipsGpRel16   = 7
	RMipsLiteral   = 8
	RMipsGot16     = 9
	RMipsPc16      = 10
	RMipsCall16    = 11
	RMipsGpRel32   = 12
	RMipsGotHi16   = 21
	RMipsGotLo16   = 22
	RMipsCallHi16  = 30
	RMipsCallLo16  = 31
	RMipsDvp27S4   = 121
	RMipsSceMhi16  = 250
	RMipsSceAddend = 251
)

const (
	STBLocal  = 0
	STBGlobal = 1
	STBWeak   = 2
)

const (
	STTNoType  = 0
	STTObject  = 1
	STTFunc    = 2
	STTSection = 3
	STTFile    = 4
)

const (
	SHNUndef  = 0
	SHNRaddr  = 0xFF1F
	SHNAbs    = 0xFFF1
	SHNCommon = 0xFFF2
)
