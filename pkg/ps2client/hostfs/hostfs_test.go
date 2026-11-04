package hostfs

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ps2build/tools/pkg/ps2client/proto"
)

func result(t *testing.T, reply []byte) int32 {
	t.Helper()
	return int32(binary.BigEndian.Uint32(reply[6:]))
}

func newServer(t *testing.T, readOnly bool) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := New(dir, readOnly)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, dir
}

func open(s *Server, p string, flags int32) int32 {
	return int32(binary.BigEndian.Uint32(s.Handle(&proto.Request{Op: proto.ReqOpen, Path: p, Flags: flags})[6:]))
}

func TestWriteReadSeek(t *testing.T) {
	s, dir := newServer(t, false)
	fd := open(s, "a.txt", 0x0002|0x0200|0x0400)
	if fd <= 0 {
		t.Fatalf("open for write returned %d", fd)
	}
	if n := result(t, s.Handle(&proto.Request{Op: proto.ReqWrite, Fd: fd, N: 5, Data: []byte("hello")})); n != 5 {
		t.Fatalf("write returned %d", n)
	}
	result(t, s.Handle(&proto.Request{Op: proto.ReqClose, Fd: fd}))
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "hello" {
		t.Fatalf("file holds %q", b)
	}

	fd = open(s, "host:a.txt", 0x0001)
	if fd <= 0 {
		t.Fatalf("open for read returned %d", fd)
	}
	if pos := result(t, s.Handle(&proto.Request{Op: proto.ReqLseek, Fd: fd, Offset: 1, Whence: 0})); pos != 1 {
		t.Fatalf("lseek returned %d", pos)
	}
	r := s.Handle(&proto.Request{Op: proto.ReqRead, Fd: fd, N: 100})
	if got := int32(binary.BigEndian.Uint32(r[6:])); got != 4 || string(r[14:]) != "ello" {
		t.Fatalf("read returned %d %q", got, r[14:])
	}
	if pos := result(t, s.Handle(&proto.Request{Op: proto.ReqLseek, Fd: fd, Offset: 0, Whence: 2})); pos != 5 {
		t.Fatalf("seek to end returned %d", pos)
	}
	r = s.Handle(&proto.Request{Op: proto.ReqRead, Fd: fd, N: 10})
	if got := int32(binary.BigEndian.Uint32(r[6:])); got != 0 || len(r) != 14 {
		t.Fatalf("read at EOF returned %d with %d bytes", got, len(r))
	}
}

func TestErrors(t *testing.T) {
	s, _ := newServer(t, false)
	if fd := open(s, "missing.txt", 0x0001); fd != -enoent {
		t.Errorf("open of a missing file returned %d", fd)
	}
	if got := result(t, s.Handle(&proto.Request{Op: proto.ReqClose, Fd: 99})); got != -ebadf {
		t.Errorf("close of a bad fd returned %d", got)
	}
	r := s.Handle(&proto.Request{Op: proto.ReqRead, Fd: 99, N: 4})
	if got := int32(binary.BigEndian.Uint32(r[10:])); got >= 0 {
		t.Errorf("read on a bad fd reported %d bytes", got)
	}
	s.Handle(&proto.Request{Op: proto.ReqMkdir, Path: "d"})
	if fd := open(s, "d", 0x0001); fd != -eisdir {
		t.Errorf("opening a directory as a file returned %d", fd)
	}
}

func TestPathsStayInsideRoot(t *testing.T) {
	s, dir := newServer(t, false)
	outside := filepath.Join(filepath.Dir(dir), "outside.txt")
	os.WriteFile(outside, []byte("secret"), 0o644)
	t.Cleanup(func() { os.Remove(outside) })

	for _, p := range []string{"../outside.txt", "..\\outside.txt", "/../../outside.txt", "host:../outside.txt", "a/../../outside.txt"} {
		if fd := open(s, p, 0x0001); fd > 0 {
			t.Errorf("%q escaped the root (fd %d)", p, fd)
		}
	}

	if runtime.GOOS != "windows" {
		if err := os.Symlink(outside, filepath.Join(dir, "link")); err == nil {
			if fd := open(s, "link", 0x0001); fd > 0 {
				t.Error("a symlink leaving the root was followed")
			}
		}
		os.Symlink(filepath.Dir(dir), filepath.Join(dir, "dirlink"))
		if fd := open(s, "dirlink/outside.txt", 0x0001); fd > 0 {
			t.Error("a directory symlink leaving the root was followed")
		}
	}
}

func TestReadOnly(t *testing.T) {
	s, dir := newServer(t, true)
	os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644)
	if fd := open(s, "f", 0x0001); fd <= 0 {
		t.Errorf("read-only server refused a read (%d)", fd)
	}
	if fd := open(s, "f", 0x0002); fd != -eacces {
		t.Errorf("write open returned %d", fd)
	}
	if fd := open(s, "new", 0x0001|0x0200); fd != -eacces {
		t.Errorf("create returned %d", fd)
	}
	for _, op := range []uint32{proto.ReqRemove, proto.ReqMkdir, proto.ReqRmdir} {
		if got := result(t, s.Handle(&proto.Request{Op: op, Path: "f"})); got != -eacces {
			t.Errorf("op 0x%X returned %d", op, got)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "f")); err != nil {
		t.Error("read-only server deleted a file")
	}
}

func TestDirectoriesAndStat(t *testing.T) {
	s, dir := newServer(t, false)
	os.WriteFile(filepath.Join(dir, "one.txt"), []byte("12345"), 0o644)
	os.Mkdir(filepath.Join(dir, "sub"), 0o755)

	h := result(t, s.Handle(&proto.Request{Op: proto.ReqOpenDir, Path: "/"}))
	if h <= 0 {
		t.Fatalf("opendir returned %d", h)
	}
	names := map[string]uint32{}
	for {
		r := s.Handle(&proto.Request{Op: proto.ReqReadDir, Fd: h})
		if int32(binary.BigEndian.Uint32(r[6:])) <= 0 {
			break
		}
		mode := binary.BigEndian.Uint32(r[10:])
		name := string(r[10+40 : 10+40+255])
		for i, c := range name {
			if c == 0 {
				name = name[:i]
				break
			}
		}
		names[name] = mode
	}
	if names["one.txt"]&0x38 != modeFile || names["sub"]&0x38 != modeDir || len(names) != 2 {
		t.Errorf("directory listing %v", names)
	}
	if got := result(t, s.Handle(&proto.Request{Op: proto.ReqCloseDir, Fd: h})); got != 0 {
		t.Errorf("closedir returned %d", got)
	}

	st := s.Handle(&proto.Request{Op: proto.ReqGetStat, Path: "one.txt"})
	if result(t, st) != 0 || binary.BigEndian.Uint32(st[10+8:]) != 5 {
		t.Errorf("getstat did not report a 5 byte file")
	}
	if result(t, s.Handle(&proto.Request{Op: proto.ReqGetStat, Path: "nope"})) != -enoent {
		t.Error("getstat of a missing file")
	}
}

func TestRemoveMkdirRmdir(t *testing.T) {
	s, dir := newServer(t, false)
	if got := result(t, s.Handle(&proto.Request{Op: proto.ReqMkdir, Path: "d"})); got != 0 {
		t.Fatalf("mkdir returned %d", got)
	}
	if got := result(t, s.Handle(&proto.Request{Op: proto.ReqMkdir, Path: "d"})); got != -eexist {
		t.Errorf("second mkdir returned %d", got)
	}
	os.WriteFile(filepath.Join(dir, "d", "f"), nil, 0o644)
	if got := result(t, s.Handle(&proto.Request{Op: proto.ReqRmdir, Path: "d"})); got == 0 {
		t.Error("rmdir removed a non-empty directory")
	}
	if got := result(t, s.Handle(&proto.Request{Op: proto.ReqRemove, Path: "d/f"})); got != 0 {
		t.Errorf("remove returned %d", got)
	}
	if got := result(t, s.Handle(&proto.Request{Op: proto.ReqRmdir, Path: "d"})); got != 0 {
		t.Errorf("rmdir returned %d", got)
	}
	if got := result(t, s.Handle(&proto.Request{Op: proto.ReqRmdir, Path: "/"})); got == 0 {
		t.Error("rmdir removed the root")
	}
}
