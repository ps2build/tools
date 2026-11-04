// Package config holds srxfixup's two section-layout tables (EE and
// IOP), describing how convert lays out a converted SRX/IRX/ERX module.
package config

import "github.com/ps2build/tools/internal/srxfixup/elf"

const (
	TargetIOP = 1
	TargetEE  = 2
)

const (
	PHTypeMod  = 1
	PHTypeText = 2
)

type SegConf struct {
	Name             string
	BitID            uint32
	SectNamePatterns []string
	EmptySection     *elf.Section
	Sections         []*elf.Section
	Addr             uint32
	Size             uint32
}

type PHeaderInfo struct {
	Type         int      // PHTypeMod or PHTypeText
	SectionName  string   // for PHTypeMod
	SegmentNames []string // for PHTypeText - resolved to *SegConf at use time via Table.Segment
}

type SectConf struct {
	Pattern  string
	Flag     uint32
	SectType uint32
	SectFlag uint32
}

type CreateSymbolConf struct {
	Name     string
	Bind     int
	Type     int
	Segment  *SegConf
	SectName string
	Shindex  int
	Seflag   int
}

type Table struct {
	Target             int
	Segments           []*SegConf
	CreateSymbols      []*CreateSymbolConf
	ProgramHeaderOrder []*PHeaderInfo
	Sections           []*SectConf
	RemoveSectionList  []string
	SectionTableOrder  []string
	FileLayoutOrder    []string
}

func (t *Table) Segment(name string) *SegConf {
	for _, s := range t.Segments {
		if s.Name == name {
			return s
		}
	}
	return nil
}
