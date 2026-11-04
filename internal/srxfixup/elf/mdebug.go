package elf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

// MipsSymbolic is the .mdebug (SHT_MIPS_DEBUG) section's ECOFF-style
// symbolic-debug header plus its sub-tables. srxfixup never interprets
// the sub-tables' contents (only the header's own Line/Dn/Pd/Sym/Opt/Aux/
// Ss/SsExt/Fd/Rfd/Ext offset fields, which it must rewrite to match
// wherever the section ends up in the output file) - so each sub-table
// is kept as an opaque byte blob and round-tripped verbatim.
type MipsSymbolic struct {
	Magic     int16
	Vstamp    int16
	ILineMax  uint32
	CbLine    uint32
	IdnMax    uint32
	IpdMax    uint32
	IsymMax   uint32
	IoptMax   uint32
	IauxMax   uint32
	IssMax    uint32
	IssExtMax uint32
	IfdMax    uint32
	Crfd      uint32
	IextMax   uint32

	HasLine  bool
	HasDn    bool
	HasPd    bool
	HasSym   bool
	HasOpt   bool
	HasAux   bool
	HasSs    bool
	HasSsExt bool
	HasFd    bool
	HasRfd   bool
	HasExt   bool

	Line  []byte
	Dn    []byte
	Pd    []byte
	Sym   []byte
	Opt   []byte
	Aux   []byte
	Ss    []byte
	SsExt []byte
	Fd    []byte
	Rfd   []byte
	Ext   []byte
}

type hdrrDisk struct {
	Magic         int16
	Vstamp        int16
	ILineMax      uint32
	CbLine        uint32
	CbLineOffset  uint32
	IdnMax        uint32
	CbDnOffset    uint32
	IpdMax        uint32
	CbPdOffset    uint32
	IsymMax       uint32
	CbSymOffset   uint32
	IoptMax       uint32
	CbOptOffset   uint32
	IauxMax       uint32
	CbAuxOffset   uint32
	IssMax        uint32
	CbSsOffset    uint32
	IssExtMax     uint32
	CbSsExtOffset uint32
	IfdMax        uint32
	CbFdOffset    uint32
	Crfd          uint32
	CbRfdOffset   uint32
	IextMax       uint32
	CbExtOffset   uint32
}

func readAt(r io.ReaderAt, off uint32, n uint32) ([]byte, error) {
	buf := make([]byte, n)
	if _, err := r.ReadAt(buf, int64(off)); err != nil {
		return nil, err
	}
	return buf, nil
}

func readMipsSymbolic(r io.ReaderAt, base int64) (*MipsSymbolic, error) {
	var h hdrrDisk
	hdr := make([]byte, 96)
	if _, err := r.ReadAt(hdr, base); err != nil {
		return nil, fmt.Errorf("read .mdebug header: %w", err)
	}
	if err := binary.Read(bytes.NewReader(hdr), binary.LittleEndian, &h); err != nil {
		return nil, err
	}
	m := &MipsSymbolic{
		Magic: h.Magic, Vstamp: h.Vstamp, ILineMax: h.ILineMax, CbLine: h.CbLine,
		IdnMax: h.IdnMax, IpdMax: h.IpdMax, IsymMax: h.IsymMax, IoptMax: h.IoptMax,
		IauxMax: h.IauxMax, IssMax: h.IssMax, IssExtMax: h.IssExtMax, IfdMax: h.IfdMax,
		Crfd: h.Crfd, IextMax: h.IextMax,
	}
	var err error
	if h.CbLineOffset > 0 {
		m.HasLine = true
		if m.Line, err = readAt(r, h.CbLineOffset, h.CbLine); err != nil {
			return nil, err
		}
	}
	if h.CbDnOffset > 0 {
		m.HasDn = true
		if m.Dn, err = readAt(r, h.CbDnOffset, 8*h.IdnMax); err != nil {
			return nil, err
		}
	}
	if h.CbPdOffset > 0 {
		m.HasPd = true
		if m.Pd, err = readAt(r, h.CbPdOffset, 52*h.IpdMax); err != nil {
			return nil, err
		}
	}
	if h.CbSymOffset > 0 {
		m.HasSym = true
		if m.Sym, err = readAt(r, h.CbSymOffset, 12*h.IsymMax); err != nil {
			return nil, err
		}
	}
	if h.CbOptOffset > 0 {
		m.HasOpt = true
		if m.Opt, err = readAt(r, h.CbOptOffset, 12*h.IoptMax); err != nil {
			return nil, err
		}
	}
	if h.CbAuxOffset > 0 {
		m.HasAux = true
		if m.Aux, err = readAt(r, h.CbAuxOffset, 4*h.IauxMax); err != nil {
			return nil, err
		}
	}
	if h.CbSsOffset > 0 {
		m.HasSs = true
		if m.Ss, err = readAt(r, h.CbSsOffset, h.IssMax); err != nil {
			return nil, err
		}
	}
	if h.CbSsExtOffset > 0 {
		m.HasSsExt = true
		if m.SsExt, err = readAt(r, h.CbSsExtOffset, h.IssExtMax); err != nil {
			return nil, err
		}
	}
	if h.CbFdOffset > 0 {
		m.HasFd = true
		if m.Fd, err = readAt(r, h.CbFdOffset, 72*h.IfdMax); err != nil {
			return nil, err
		}
	}
	if h.CbRfdOffset > 0 {
		m.HasRfd = true
		if m.Rfd, err = readAt(r, h.CbRfdOffset, 4*h.Crfd); err != nil {
			return nil, err
		}
	}
	if h.CbExtOffset > 0 {
		m.HasExt = true
		if m.Ext, err = readAt(r, h.CbExtOffset, 16*h.IextMax); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// writeMipsSymbolic writes the header + sub-tables starting at basepos,
// recomputing each present sub-table's offset sequentially from
// basepos+96.
func writeMipsSymbolic(w io.WriterAt, basepos uint32, m *MipsSymbolic) error {
	h := hdrrDisk{
		Magic: m.Magic, Vstamp: m.Vstamp, ILineMax: m.ILineMax, CbLine: m.CbLine,
		IdnMax: m.IdnMax, IpdMax: m.IpdMax, IsymMax: m.IsymMax, IoptMax: m.IoptMax,
		IauxMax: m.IauxMax, IssMax: m.IssMax, IssExtMax: m.IssExtMax, IfdMax: m.IfdMax,
		Crfd: m.Crfd, IextMax: m.IextMax,
	}
	pos := basepos + 96
	writeSub := func(has bool, data []byte, offsetOut *uint32) error {
		if !has {
			return nil
		}
		*offsetOut = pos
		if _, err := w.WriteAt(data, int64(pos)); err != nil {
			return err
		}
		pos += uint32(len(data))
		return nil
	}
	if err := writeSub(m.HasLine, m.Line, &h.CbLineOffset); err != nil {
		return err
	}
	if err := writeSub(m.HasDn, m.Dn, &h.CbDnOffset); err != nil {
		return err
	}
	if err := writeSub(m.HasPd, m.Pd, &h.CbPdOffset); err != nil {
		return err
	}
	if err := writeSub(m.HasSym, m.Sym, &h.CbSymOffset); err != nil {
		return err
	}
	if m.HasOpt {
		if _, err := w.WriteAt(m.Opt, int64(pos)); err != nil {
			return err
		}
		pos += uint32(len(m.Opt))
	}
	if err := writeSub(m.HasAux, m.Aux, &h.CbAuxOffset); err != nil {
		return err
	}
	if err := writeSub(m.HasSs, m.Ss, &h.CbSsOffset); err != nil {
		return err
	}
	if err := writeSub(m.HasSsExt, m.SsExt, &h.CbSsExtOffset); err != nil {
		return err
	}
	if err := writeSub(m.HasFd, m.Fd, &h.CbFdOffset); err != nil {
		return err
	}
	if err := writeSub(m.HasRfd, m.Rfd, &h.CbRfdOffset); err != nil {
		return err
	}
	if err := writeSub(m.HasExt, m.Ext, &h.CbExtOffset); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := binary.Write(&buf, binary.LittleEndian, &h); err != nil {
		return err
	}
	_, err := w.WriteAt(buf.Bytes(), int64(basepos))
	return err
}
