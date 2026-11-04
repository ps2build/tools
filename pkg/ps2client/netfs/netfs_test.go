package netfs

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

type fakeServer struct {
	conn  net.Conn
	files map[string][]byte
	dirs  map[string][]string
	open  map[int32]*fakeFile
	rd    map[int32][]string
	next  int32
}

type fakeFile struct {
	path string
	pos  int
	data []byte
}

func (s *fakeServer) reply(cmd uint32, body ...[]byte) {
	var b []byte
	for _, p := range body {
		b = append(b, p...)
	}
	h := make([]byte, hdrLen)
	binary.BigEndian.PutUint32(h, cmd+1)
	binary.BigEndian.PutUint16(h[4:], uint16(hdrLen+len(b)))
	s.conn.Write(append(h, b...))
}

func ret(v int32) []byte { return i32(v) }

func (s *fakeServer) serve() {
	defer s.conn.Close()
	for {
		var h [hdrLen]byte
		if _, err := io.ReadFull(s.conn, h[:]); err != nil {
			return
		}
		cmd := binary.BigEndian.Uint32(h[:])
		body := make([]byte, int(binary.BigEndian.Uint16(h[4:]))-hdrLen)
		io.ReadFull(s.conn, body)
		arg := func(i int) int32 { return int32(binary.BigEndian.Uint32(body[4*i:])) }
		switch cmd {
		case cmdOpen:
			p := cstr(body[4:])
			if _, ok := s.files[p]; !ok && arg(0)&OCreate == 0 {
				s.reply(cmd, ret(-2))
				continue
			}
			if arg(0)&OTrunc != 0 {
				s.files[p] = nil
			}
			s.next++
			s.open[s.next] = &fakeFile{path: p, data: s.files[p]}
			s.reply(cmd, ret(s.next))
		case cmdClose:
			f := s.open[arg(0)]
			s.files[f.path] = f.data
			delete(s.open, arg(0))
			s.reply(cmd, ret(0))
		case cmdRead:
			f := s.open[arg(0)]
			end := min(f.pos+int(arg(1)), len(f.data))
			part := f.data[f.pos:end]
			f.pos = end
			s.reply(cmd, ret(int32(len(part))), ret(int32(len(part))))
			if len(part) > 0 {
				s.conn.Write(part)
			}
		case cmdWrite:
			f := s.open[arg(0)]
			data := make([]byte, arg(1))
			io.ReadFull(s.conn, data)
			f.data = append(f.data, data...)
			s.reply(cmd, ret(int32(len(data))))
		case cmdMkdir:
			s.dirs[cstr(body[4:])] = nil
			s.reply(cmd, ret(0))
		case cmdRemove:
			p := cstr(body[4:])
			if _, ok := s.files[p]; !ok {
				s.reply(cmd, ret(-2))
				continue
			}
			delete(s.files, p)
			s.reply(cmd, ret(0))
		case cmdDopen:
			names, ok := s.dirs[cstr(body[4:])]
			if !ok {
				s.reply(cmd, ret(-2))
				continue
			}
			s.next++
			s.rd[s.next] = names
			s.reply(cmd, ret(s.next))
		case cmdDread:
			names := s.rd[arg(0)]
			e := make([]byte, direntLen)
			if len(names) == 0 {
				s.reply(cmd, e)
				continue
			}
			s.rd[arg(0)] = names[1:]
			binary.BigEndian.PutUint32(e, 1)
			mode := uint32(ModeFile)
			if strings.HasSuffix(names[0], "/") {
				mode = ModeDir
			}
			binary.BigEndian.PutUint32(e[4:], mode)
			binary.BigEndian.PutUint32(e[12:], 42)
			copy(e[16:], []byte{0, 5, 4, 3, 2, 1, 0xea, 0x07})
			copy(e[44:], strings.TrimSuffix(names[0], "/"))
			s.reply(cmd, e)
		case cmdDclose:
			delete(s.rd, arg(0))
			s.reply(cmd, ret(0))
		case cmdDevlist:
			l := make([]byte, pathLen)
			copy(l, "host,mc0,mass")
			s.reply(cmd, ret(3), ret(3), l)
		case cmdUmount:
			s.reply(cmd, ret(-19))
		}
	}
}

func newPair(t *testing.T) (*Client, *fakeServer) {
	t.Helper()
	a, b := net.Pipe()
	s := &fakeServer{
		conn:  b,
		files: map[string][]byte{},
		dirs:  map[string][]string{},
		open:  map[int32]*fakeFile{},
		rd:    map[int32][]string{},
	}
	go s.serve()
	c := New(a, 2*time.Second)
	t.Cleanup(func() { c.Close() })
	return c, s
}

func TestUploadThenDownloadLargeFile(t *testing.T) {
	c, _ := newPair(t)
	want := bytes.Repeat([]byte("ps2"), 40000)
	if n, err := c.Upload("mc0:/big.bin", bytes.NewReader(want)); err != nil || n != int64(len(want)) {
		t.Fatalf("upload %d, %v", n, err)
	}
	var got bytes.Buffer
	if n, err := c.Download("mc0:/big.bin", &got); err != nil || n != int64(len(want)) {
		t.Fatalf("download %d, %v", n, err)
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Fatal("content differs")
	}
}

func TestOpenMissingFileReturnsErrno(t *testing.T) {
	c, _ := newPair(t)
	_, err := c.Open("mc0:/nope", ORead)
	var e *Error
	if !errors.As(err, &e) || e.Errno != 2 {
		t.Fatalf("got %v", err)
	}
	if e.Error() != "no such file or directory" {
		t.Fatalf("text %q", e.Error())
	}
}

func TestListDecodesEntries(t *testing.T) {
	c, s := newPair(t)
	s.dirs["mc0:/"] = []string{"a.txt", "sub/"}
	got, err := c.List("mc0:/")
	if err != nil || len(got) != 2 {
		t.Fatalf("%v %v", got, err)
	}
	if got[0].Name != "a.txt" || got[0].Size != 42 || got[0].IsDir() {
		t.Fatalf("entry 0: %+v", got[0])
	}
	if !got[1].IsDir() {
		t.Fatalf("entry 1: %+v", got[1])
	}
	want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if !got[0].Created.Equal(want) {
		t.Fatalf("created %v want %v", got[0].Created, want)
	}
}

func TestDevicesSplitsList(t *testing.T) {
	c, _ := newPair(t)
	got, err := c.Devices()
	if err != nil || strings.Join(got, "|") != "host|mc0|mass" {
		t.Fatalf("%v %v", got, err)
	}
}

func TestRemoveAndErrorPropagation(t *testing.T) {
	c, s := newPair(t)
	s.files["mc0:/x"] = []byte("x")
	if err := c.Remove("mc0:/x"); err != nil {
		t.Fatal(err)
	}
	if err := c.Remove("mc0:/x"); err == nil {
		t.Fatal("second remove should fail")
	}
	if err := c.Umount("hdd0:"); err == nil || err.Error() != "no such device" {
		t.Fatalf("umount: %v", err)
	}
}

func TestPathTooLongIsRejectedBeforeSending(t *testing.T) {
	c, _ := newPair(t)
	if err := c.Mkdir(strings.Repeat("a", 256)); err == nil {
		t.Fatal("expected error")
	}
}

func TestMountEncodesFields(t *testing.T) {
	a, b := net.Pipe()
	c := New(a, time.Second)
	defer c.Close()
	go func() {
		defer b.Close()
		var h [hdrLen]byte
		io.ReadFull(b, h[:])
		n := int(binary.BigEndian.Uint16(h[4:])) - hdrLen
		body := make([]byte, n)
		io.ReadFull(b, body)
		ok := binary.BigEndian.Uint32(h[:]) == cmdMount && n == 3*pathLen+8 &&
			cstr(body) == "pfs:" && cstr(body[pathLen:]) == "hdd0:__common" &&
			binary.BigEndian.Uint32(body[2*pathLen:]) == 1 &&
			binary.BigEndian.Uint32(body[3*pathLen+4:]) == 3
		code := int32(0)
		if !ok {
			code = -22
		}
		rep := make([]byte, hdrLen)
		binary.BigEndian.PutUint32(rep, cmdMount+1)
		binary.BigEndian.PutUint16(rep[4:], hdrLen+4)
		b.Write(append(rep, ret(code)...))
	}()
	if err := c.Mount("pfs:", "hdd0:__common", 1, []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
}
