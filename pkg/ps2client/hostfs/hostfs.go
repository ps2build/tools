// Package hostfs answers the PS2's host: file requests from a directory on the PC.
package hostfs

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ps2build/tools/pkg/ps2client/proto"
)

// Error codes returned to the PS2 (negated).
const (
	eperm     = 1
	enoent    = 2
	eio       = 5
	ebadf     = 9
	eacces    = 13
	eexist    = 17
	enotdir   = 20
	eisdir    = 21
	einval    = 22
	enotempty = 39
	eoverflow = 75
)

// PS2 fileio open flags.
const (
	oRead   = 0x0001
	oWrite  = 0x0002
	oAppend = 0x0100
	oCreate = 0x0200
	oTrunc  = 0x0400
	oExcl   = 0x0800
)

// Server serves file requests below Root.
type Server struct {
	Root     string
	ReadOnly bool
	// Log, if set, receives a line per request.
	Log func(format string, args ...any)

	root  string
	next  int32
	files map[int32]*os.File
	dirs  map[int32]*dirHandle
}

type dirHandle struct {
	full    string
	entries []fs.DirEntry
	pos     int
}

// New returns a server rooted at dir.
func New(dir string, readOnly bool) (*Server, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(real)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, errors.New(dir + " is not a directory")
	}
	return &Server{
		Root: abs, ReadOnly: readOnly, root: real, next: 1,
		files: map[int32]*os.File{}, dirs: map[int32]*dirHandle{},
	}, nil
}

// Close releases every open handle.
func (s *Server) Close() {
	for fd, f := range s.files {
		f.Close()
		delete(s.files, fd)
	}
	for h := range s.dirs {
		delete(s.dirs, h)
	}
}

func (s *Server) logf(format string, args ...any) {
	if s.Log != nil {
		s.Log(format, args...)
	}
}

// errno converts a Go error into a negative PS2 result.
func errno(err error) int32 {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, fs.ErrNotExist):
		return -enoent
	case errors.Is(err, fs.ErrExist):
		return -eexist
	case errors.Is(err, fs.ErrPermission):
		return -eacces
	case errors.Is(err, syscall.ENOTEMPTY):
		return -enotempty
	case errors.Is(err, syscall.EISDIR):
		return -eisdir
	case errors.Is(err, syscall.ENOTDIR):
		return -enotdir
	case errors.Is(err, fs.ErrInvalid):
		return -einval
	}
	return -eio
}

// resolve maps a PS2 path to a host path inside the root, refusing anything
// that would leave it, including through symbolic links.
func (s *Server) resolve(p string) (string, int32) {
	p = strings.ReplaceAll(p, "\\", "/")
	if i := strings.IndexByte(p, ':'); i >= 0 && !strings.Contains(p[:i], "/") {
		p = p[i+1:]
	}
	clean := path.Clean("/" + p)
	full := filepath.Join(s.root, filepath.FromSlash(clean))
	if clean == "/" {
		return full, 0
	}
	// The parent must exist and stay inside the root once links are resolved;
	// the final element may be missing (create) or itself a link (removed as a link).
	parent, err := filepath.EvalSymlinks(filepath.Dir(full))
	if err != nil {
		return "", errno(err)
	}
	if parent != s.root && !strings.HasPrefix(parent, s.root+string(filepath.Separator)) {
		return "", -eacces
	}
	if real, err := filepath.EvalSymlinks(full); err == nil {
		if real != s.root && !strings.HasPrefix(real, s.root+string(filepath.Separator)) {
			return "", -eacces
		}
	}
	return full, 0
}

func (s *Server) alloc() int32 {
	for {
		id := s.next
		s.next++
		if s.next <= 0 {
			s.next = 1
		}
		if _, used := s.files[id]; used {
			continue
		}
		if _, used := s.dirs[id]; used {
			continue
		}
		return id
	}
}

// The PS2's ioman reports type in bits 0x38 and permissions as a single
// read/write/execute triple.
const (
	modeLink = 0x08
	modeFile = 0x10
	modeDir  = 0x20
	modeR    = 0x04
	modeW    = 0x02
	modeX    = 0x01
)

func permBits(m fs.FileMode) uint32 {
	var out uint32
	if m&0o444 != 0 {
		out |= modeR
	}
	if m&0o222 != 0 {
		out |= modeW
	}
	if m&0o111 != 0 {
		out |= modeX
	}
	return out
}

func ps2Time(t time.Time) [8]byte {
	var b [8]byte
	b[1] = byte(t.Second())
	b[2] = byte(t.Minute())
	b[3] = byte(t.Hour())
	b[4] = byte(t.Day())
	b[5] = byte(t.Month())
	b[6] = byte(t.Year())
	b[7] = byte(t.Year() >> 8)
	return b
}

func statOf(fi fs.FileInfo) proto.Stat {
	mode := permBits(fi.Mode().Perm())
	switch {
	case fi.IsDir():
		mode |= modeDir
	case fi.Mode()&fs.ModeSymlink != 0:
		mode |= modeLink
	default:
		mode |= modeFile
	}
	t := ps2Time(fi.ModTime())
	size := uint64(0)
	if !fi.IsDir() {
		size = uint64(fi.Size())
	}
	return proto.Stat{Mode: mode, Size: size, Ctime: t, Atime: t, Mtime: t}
}

var opNames = map[uint32]string{
	proto.ReqOpen: "open", proto.ReqClose: "close", proto.ReqRead: "read", proto.ReqWrite: "write",
	proto.ReqLseek: "lseek", proto.ReqOpenDir: "opendir", proto.ReqCloseDir: "closedir",
	proto.ReqReadDir: "readdir", proto.ReqRemove: "remove", proto.ReqMkdir: "mkdir",
	proto.ReqRmdir: "rmdir", proto.ReqGetStat: "getstat",
}

// Handle answers one request and returns the bytes to send back.
func (s *Server) Handle(r *proto.Request) []byte {
	reply := s.handle(r)
	if s.Log != nil {
		name := opNames[r.Op]
		if name == "" {
			name = fmt.Sprintf("0x%08X", r.Op)
		}
		s.Log("%s path=%q fd=%d n=%d flags=0x%X off=%d whence=%d mode=0x%X -> %d", name, r.Path, r.Fd, r.N, r.Flags, r.Offset, r.Whence, r.Mode, int32(binary.BigEndian.Uint32(reply[6:])))
	}
	return reply
}

func (s *Server) handle(r *proto.Request) []byte {
	switch r.Op {
	case proto.ReqOpen:
		return proto.ReplyResult(r.Op, s.open(r))
	case proto.ReqClose:
		return proto.ReplyResult(r.Op, s.closeFile(r.Fd))
	case proto.ReqRead:
		return s.read(r)
	case proto.ReqWrite:
		return proto.ReplyResult(r.Op, s.write(r))
	case proto.ReqLseek:
		return proto.ReplyResult(r.Op, s.lseek(r))
	case proto.ReqOpenDir:
		return proto.ReplyResult(r.Op, s.openDir(r))
	case proto.ReqCloseDir:
		return proto.ReplyResult(r.Op, s.closeDir(r.Fd))
	case proto.ReqReadDir:
		return s.readDir(r)
	case proto.ReqRemove:
		return proto.ReplyResult(r.Op, s.remove(r))
	case proto.ReqMkdir:
		return proto.ReplyResult(r.Op, s.mkdir(r))
	case proto.ReqRmdir:
		return proto.ReplyResult(r.Op, s.rmdir(r))
	case proto.ReqGetStat:
		return s.getStat(r)
	}
	s.logf("unknown request 0x%08X", r.Op)
	return proto.ReplyResult(r.Op, -einval)
}

func (s *Server) open(r *proto.Request) int32 {
	access := r.Flags & 3
	writing := access&oWrite != 0 || r.Flags&(oCreate|oTrunc|oAppend) != 0
	s.logf("open %q flags=0x%X", r.Path, r.Flags)
	if writing && s.ReadOnly {
		return -eacces
	}
	full, e := s.resolve(r.Path)
	if e != 0 {
		return e
	}
	var flag int
	switch access {
	case oRead:
		flag = os.O_RDONLY
	case oWrite:
		flag = os.O_WRONLY
	case oRead | oWrite:
		flag = os.O_RDWR
	default:
		return -einval
	}
	if r.Flags&oAppend != 0 {
		flag |= os.O_APPEND
	}
	if r.Flags&oCreate != 0 {
		flag |= os.O_CREATE
	}
	if r.Flags&oTrunc != 0 {
		flag |= os.O_TRUNC
	}
	if r.Flags&oExcl != 0 {
		flag |= os.O_EXCL
	}
	f, err := os.OpenFile(full, flag, 0o666)
	if err != nil {
		return errno(err)
	}
	if fi, err := f.Stat(); err == nil && fi.IsDir() {
		f.Close()
		return -eisdir
	}
	id := s.alloc()
	s.files[id] = f
	return id
}

func (s *Server) closeFile(fd int32) int32 {
	f, ok := s.files[fd]
	if !ok {
		return -ebadf
	}
	delete(s.files, fd)
	return errno(f.Close())
}

const maxRead = 16 << 20

func (s *Server) read(r *proto.Request) []byte {
	f, ok := s.files[r.Fd]
	if !ok || r.N < 0 {
		return proto.ReplyRead(-ebadf, nil)
	}
	n := int(r.N)
	if n > maxRead {
		n = maxRead
	}
	buf := make([]byte, n)
	got, err := io.ReadFull(f, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return proto.ReplyRead(errno(err), nil)
	}
	return proto.ReplyRead(int32(got), buf[:got])
}

func (s *Server) write(r *proto.Request) int32 {
	f, ok := s.files[r.Fd]
	if !ok {
		return -ebadf
	}
	if s.ReadOnly {
		return -eacces
	}
	n, err := f.Write(r.Data)
	if err != nil && n == 0 {
		return errno(err)
	}
	return int32(n)
}

func (s *Server) lseek(r *proto.Request) int32 {
	f, ok := s.files[r.Fd]
	if !ok {
		return -ebadf
	}
	if r.Whence < 0 || r.Whence > 2 {
		return -einval
	}
	pos, err := f.Seek(int64(r.Offset), int(r.Whence))
	if err != nil {
		return errno(err)
	}
	if pos > 0x7FFFFFFF {
		return -eoverflow
	}
	return int32(pos)
}

func (s *Server) openDir(r *proto.Request) int32 {
	s.logf("opendir %q", r.Path)
	full, e := s.resolve(r.Path)
	if e != 0 {
		return e
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		return errno(err)
	}
	id := s.alloc()
	s.dirs[id] = &dirHandle{full: full, entries: entries}
	return id
}

func (s *Server) closeDir(h int32) int32 {
	if _, ok := s.dirs[h]; !ok {
		return -ebadf
	}
	delete(s.dirs, h)
	return 0
}

func (s *Server) readDir(r *proto.Request) []byte {
	d, ok := s.dirs[r.Fd]
	if !ok {
		return proto.ReplyReadDir(-ebadf, proto.Stat{}, "")
	}
	for d.pos < len(d.entries) {
		e := d.entries[d.pos]
		d.pos++
		fi, err := e.Info()
		if err != nil {
			continue
		}
		return proto.ReplyReadDir(1, statOf(fi), e.Name())
	}
	return proto.ReplyReadDir(0, proto.Stat{}, "")
}

func (s *Server) remove(r *proto.Request) int32 {
	s.logf("remove %q", r.Path)
	if s.ReadOnly {
		return -eacces
	}
	full, e := s.resolve(r.Path)
	if e != 0 {
		return e
	}
	fi, err := os.Lstat(full)
	if err != nil {
		return errno(err)
	}
	if fi.IsDir() {
		return -eisdir
	}
	return errno(os.Remove(full))
}

func (s *Server) mkdir(r *proto.Request) int32 {
	s.logf("mkdir %q", r.Path)
	if s.ReadOnly {
		return -eacces
	}
	full, e := s.resolve(r.Path)
	if e != 0 {
		return e
	}
	return errno(os.Mkdir(full, 0o777))
}

func (s *Server) rmdir(r *proto.Request) int32 {
	s.logf("rmdir %q", r.Path)
	if s.ReadOnly {
		return -eacces
	}
	full, e := s.resolve(r.Path)
	if e != 0 {
		return e
	}
	if full == s.root {
		return -eperm
	}
	fi, err := os.Lstat(full)
	if err != nil {
		return errno(err)
	}
	if !fi.IsDir() {
		return -enotdir
	}
	return errno(os.Remove(full))
}

func (s *Server) getStat(r *proto.Request) []byte {
	full, e := s.resolve(r.Path)
	if e != 0 {
		return proto.ReplyGetStat(e, proto.Stat{})
	}
	fi, err := os.Stat(full)
	if err != nil {
		return proto.ReplyGetStat(errno(err), proto.Stat{})
	}
	return proto.ReplyGetStat(0, statOf(fi))
}
