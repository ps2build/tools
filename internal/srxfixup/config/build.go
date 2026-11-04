package config

import "github.com/ps2build/tools/internal/srxfixup/elf"

func makeEmptySection(name string, typ, flag uint32) *elf.Section {
	s := &elf.Section{Name: name}
	s.Shdr.Type = typ
	s.Shdr.Flags = flag
	s.Shdr.Size = 0
	s.Shdr.Addralign = 4
	s.Shdr.Entsize = 0
	return s
}
