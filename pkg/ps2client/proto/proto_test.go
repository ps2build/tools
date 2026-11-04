package proto

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func TestCommandSizes(t *testing.T) {
	exec, err := Exec(CmdExecIOP, "host:a.irx", []string{"x", "y"})
	if err != nil {
		t.Fatal(err)
	}
	if len(exec) != 266 {
		t.Errorf("execiop packet is %d bytes, want 266", len(exec))
	}
	if got := binary.BigEndian.Uint16(exec[4:]); got != 266 {
		t.Errorf("execiop length field %d", got)
	}
	if got := binary.BigEndian.Uint32(exec[6:]); got != 3 {
		t.Errorf("argc %d, want 3", got)
	}
	if !bytes.Equal(exec[10:10+len("host:a.irx\x00x\x00y\x00")], []byte("host:a.irx\x00x\x00y\x00")) {
		t.Errorf("argv not packed as NUL separated strings: %q", exec[10:30])
	}

	mem, err := Mem(CmdDumpMem, 0x100000, 64, "out.bin")
	if err != nil {
		t.Fatal(err)
	}
	if len(mem) != 270 {
		t.Errorf("mem packet is %d bytes, want 270", len(mem))
	}
	if binary.BigEndian.Uint32(mem[6:]) != 0x100000 || binary.BigEndian.Uint32(mem[10:]) != 64 {
		t.Errorf("mem offset or size wrong")
	}

	if got := len(Simple(CmdReset)); got != 6 {
		t.Errorf("reset is %d bytes", got)
	}
	if got := len(VU(CmdStartVU, 1)); got != 10 {
		t.Errorf("startvu is %d bytes", got)
	}
	gs, _ := GSExec(48, "x.gs")
	if len(gs) != 6+2+256 {
		t.Errorf("gsexec is %d bytes", len(gs))
	}
	dr, _ := DumpReg(RegGS, "r.bin")
	if len(dr) != 6+4+256 {
		t.Errorf("dumpreg is %d bytes", len(dr))
	}
}

func TestPathLimits(t *testing.T) {
	if _, err := Mem(CmdDumpMem, 0, 0, strings.Repeat("a", 256)); err == nil {
		t.Error("256-byte path accepted")
	}
	if _, err := Mem(CmdDumpMem, 0, 0, strings.Repeat("a", 255)); err != nil {
		t.Errorf("255-byte path rejected: %v", err)
	}
	if _, err := Exec(CmdExecEE, strings.Repeat("a", 200), []string{strings.Repeat("b", 100)}); err == nil {
		t.Error("oversized argument list accepted")
	}
	if _, err := Mem(CmdDumpMem, 0, 0, "a\x00b"); err == nil {
		t.Error("NUL in path accepted")
	}
}

func req(op uint32, payload []byte) []byte {
	b := make([]byte, 6+len(payload))
	binary.BigEndian.PutUint32(b, op)
	binary.BigEndian.PutUint16(b[4:], uint16(len(b)))
	copy(b[6:], payload)
	return b
}

func TestReadRequests(t *testing.T) {
	open := make([]byte, 4+256)
	binary.BigEndian.PutUint32(open, 0x0201)
	copy(open[4:], "dir/file.txt")
	r, err := ReadRequest(bytes.NewReader(req(ReqOpen, open)))
	if err != nil {
		t.Fatal(err)
	}
	if r.Op != ReqOpen || r.Flags != 0x0201 || r.Path != "dir/file.txt" {
		t.Errorf("open decoded as %+v", r)
	}

	w := make([]byte, 8)
	binary.BigEndian.PutUint32(w, 7)
	binary.BigEndian.PutUint32(w[4:], 5)
	stream := append(req(ReqWrite, w), []byte("hello")...)
	stream = append(stream, req(ReqClose, []byte{0, 0, 0, 7})...)
	rd := bytes.NewReader(stream)
	r, err = ReadRequest(rd)
	if err != nil {
		t.Fatal(err)
	}
	if r.Fd != 7 || string(r.Data) != "hello" {
		t.Errorf("write decoded as %+v", r)
	}
	r, err = ReadRequest(rd)
	if err != nil || r.Op != ReqClose || r.Fd != 7 {
		t.Errorf("request after a write was misread: %+v %v", r, err)
	}
}

func TestRejectsBadLengths(t *testing.T) {
	b := make([]byte, 6)
	binary.BigEndian.PutUint32(b, ReqClose)
	binary.BigEndian.PutUint16(b[4:], 3)
	if _, err := ReadRequest(bytes.NewReader(b)); err == nil {
		t.Error("length below the header size accepted")
	}
	binary.BigEndian.PutUint16(b[4:], 0xFFFF)
	if _, err := ReadRequest(bytes.NewReader(b)); err == nil {
		t.Error("oversized length accepted")
	}
	if _, err := ReadRequest(bytes.NewReader(req(ReqOpen, []byte{0, 0, 0, 1}))); err == nil {
		t.Error("open without a path accepted")
	}
}

func TestReplySizes(t *testing.T) {
	check := func(name string, b []byte, wantLen, wantField int) {
		t.Helper()
		if len(b) != wantLen {
			t.Errorf("%s is %d bytes, want %d", name, len(b), wantLen)
		}
		if got := int(binary.BigEndian.Uint16(b[4:])); got != wantField {
			t.Errorf("%s length field %d, want %d", name, got, wantField)
		}
	}
	check("result", ReplyResult(ReqOpen, 3), 10, 10)
	if op := binary.BigEndian.Uint32(ReplyResult(ReqOpen, 3)); op != ReqOpen+1 {
		t.Errorf("reply opcode 0x%X", op)
	}
	check("read", ReplyRead(4, []byte("abcd")), 14+4, 14)
	check("getstat", ReplyGetStat(0, Stat{}), 50, 50)
	check("readdir", ReplyReadDir(1, Stat{}, "a"), 306, 306)
}

func TestReadErrorCarriesNegativeCount(t *testing.T) {
	b := ReplyRead(-9, nil)
	if got := int32(binary.BigEndian.Uint32(b[10:])); got != -9 {
		t.Errorf("byte count %d, want -9", got)
	}
}

func TestStatLayout(t *testing.T) {
	s := Stat{Mode: 0x2124, Size: 0x1_0000_0002}
	b := ReplyGetStat(0, s)
	body := b[10:]
	if binary.BigEndian.Uint32(body[0:]) != 0x2124 {
		t.Error("mode")
	}
	if binary.BigEndian.Uint32(body[8:]) != 2 {
		t.Error("size low word")
	}
	if binary.BigEndian.Uint32(body[36:]) != 1 {
		t.Error("size high word")
	}
}

func TestMkdirModeIsLittleEndian(t *testing.T) {
	payload := make([]byte, 4+256)
	binary.LittleEndian.PutUint32(payload, 0x1ED)
	copy(payload[4:], "d")
	r, err := ReadRequest(bytes.NewReader(req(ReqMkdir, payload)))
	if err != nil {
		t.Fatal(err)
	}
	if r.Mode != 0x1ED || r.Path != "d" {
		t.Errorf("mkdir decoded as %+v", r)
	}
}
