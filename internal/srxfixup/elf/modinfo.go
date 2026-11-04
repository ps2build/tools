package elf

import (
	"encoding/binary"
	"fmt"
)

// iopModFixedSize/eeModFixedSize are the sum of each struct's fields
// before its flexible modulename[] array (moduleinfo through
// moduleversion).
const (
	iopModFixedSize = 4 + 4 + 4 + 4 + 4 + 4 + 2 // moduleinfo..moduleversion
	eeModFixedSize  = 4*10 + 2                  // moduleinfo..moduleversion
)

const (
	iopModStructSizeof = iopModFixedSize + 2
	eeModStructSizeof  = eeModFixedSize + 2
)

func IopModHeaderSize() uint32 { return iopModStructSizeof }
func EeModHeaderSize() uint32  { return eeModStructSizeof }

func (m *IopMod) size() uint32 {
	return uint32(iopModFixedSize + len(m.ModuleName) + 1)
}

func (m *EeMod) size() uint32 {
	return uint32(eeModFixedSize + len(m.ModuleName) + 1)
}

func unmarshalIopMod(b []byte) (*IopMod, error) {
	if len(b) < iopModFixedSize {
		return nil, fmt.Errorf("truncated .iopmod section (%d bytes)", len(b))
	}
	m := &IopMod{
		ModuleInfo:    binary.LittleEndian.Uint32(b[0:4]),
		Entry:         binary.LittleEndian.Uint32(b[4:8]),
		GPValue:       binary.LittleEndian.Uint32(b[8:12]),
		TextSize:      binary.LittleEndian.Uint32(b[12:16]),
		DataSize:      binary.LittleEndian.Uint32(b[16:20]),
		BssSize:       binary.LittleEndian.Uint32(b[20:24]),
		ModuleVersion: binary.LittleEndian.Uint16(b[24:26]),
		ModuleName:    cstrBytes(b[26:]),
	}
	return m, nil
}

func unmarshalEeMod(b []byte) (*EeMod, error) {
	if len(b) < eeModFixedSize {
		return nil, fmt.Errorf("truncated .eemod section (%d bytes)", len(b))
	}
	m := &EeMod{
		ModuleInfo:    binary.LittleEndian.Uint32(b[0:4]),
		Entry:         binary.LittleEndian.Uint32(b[4:8]),
		GPValue:       binary.LittleEndian.Uint32(b[8:12]),
		TextSize:      binary.LittleEndian.Uint32(b[12:16]),
		DataSize:      binary.LittleEndian.Uint32(b[16:20]),
		BssSize:       binary.LittleEndian.Uint32(b[20:24]),
		ErxLibAddr:    binary.LittleEndian.Uint32(b[24:28]),
		ErxLibSize:    binary.LittleEndian.Uint32(b[28:32]),
		ErxStubAddr:   binary.LittleEndian.Uint32(b[32:36]),
		ErxStubSize:   binary.LittleEndian.Uint32(b[36:40]),
		ModuleVersion: binary.LittleEndian.Uint16(b[40:42]),
		ModuleName:    cstrBytes(b[42:]),
	}
	return m, nil
}

func marshalIopMod(m *IopMod) []byte {
	b := make([]byte, iopModFixedSize+len(m.ModuleName)+1)
	binary.LittleEndian.PutUint32(b[0:4], m.ModuleInfo)
	binary.LittleEndian.PutUint32(b[4:8], m.Entry)
	binary.LittleEndian.PutUint32(b[8:12], m.GPValue)
	binary.LittleEndian.PutUint32(b[12:16], m.TextSize)
	binary.LittleEndian.PutUint32(b[16:20], m.DataSize)
	binary.LittleEndian.PutUint32(b[20:24], m.BssSize)
	binary.LittleEndian.PutUint16(b[24:26], m.ModuleVersion)
	copy(b[26:], m.ModuleName)
	return b
}

func marshalEeMod(m *EeMod) []byte {
	b := make([]byte, eeModFixedSize+len(m.ModuleName)+1)
	binary.LittleEndian.PutUint32(b[0:4], m.ModuleInfo)
	binary.LittleEndian.PutUint32(b[4:8], m.Entry)
	binary.LittleEndian.PutUint32(b[8:12], m.GPValue)
	binary.LittleEndian.PutUint32(b[12:16], m.TextSize)
	binary.LittleEndian.PutUint32(b[16:20], m.DataSize)
	binary.LittleEndian.PutUint32(b[20:24], m.BssSize)
	binary.LittleEndian.PutUint32(b[24:28], m.ErxLibAddr)
	binary.LittleEndian.PutUint32(b[28:32], m.ErxLibSize)
	binary.LittleEndian.PutUint32(b[32:36], m.ErxStubAddr)
	binary.LittleEndian.PutUint32(b[36:40], m.ErxStubSize)
	binary.LittleEndian.PutUint16(b[40:42], m.ModuleVersion)
	copy(b[42:], m.ModuleName)
	return b
}

func cstrBytes(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

// IopMod returns the section's parsed .iopmod struct, or nil if this
// isn't an SHT_SCE_IOPMOD section.
func (s *Section) IopMod() *IopMod { return s.iopMod }

// EeMod returns the section's parsed .eemod struct, or nil if this
// isn't an SHT_SCE_EEMOD section.
func (s *Section) EeMod() *EeMod { return s.eeMod }

// SetIopMod installs m as this section's module-info data and updates
// Shdr.Size to match.
func (s *Section) SetIopMod(m *IopMod) {
	s.iopMod = m
	s.Shdr.Size = m.size()
}

// SetEeMod is SetIopMod's .eemod counterpart.
func (s *Section) SetEeMod(m *EeMod) {
	s.eeMod = m
	s.Shdr.Size = m.size()
}

// NewIopModSection returns a fresh, empty .iopmod section with
// moduleinfo defaulted to -1 (SHN-style "not yet resolved" sentinel).
func NewIopModSection() *Section {
	m := &IopMod{ModuleInfo: 0xFFFFFFFF}
	s := &Section{Name: ".iopmod", iopMod: m}
	s.Shdr.Type = SHTSceIopMod
	s.Shdr.Addralign = 4
	s.Shdr.Size = m.size()
	return s
}

func NewEeModSection() *Section {
	m := &EeMod{ModuleInfo: 0xFFFFFFFF}
	s := &Section{Name: ".eemod", eeMod: m}
	s.Shdr.Type = SHTSceEeMod
	s.Shdr.Addralign = 4
	s.Shdr.Size = m.size()
	return s
}
