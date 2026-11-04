// Package netfs is a client for the PS2's ps2netfs file server (TCP port 0x4713).
package netfs

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// Port is the TCP port ps2netfs listens on.
const Port = 0x4713

const (
	hdrLen  = 6
	pathLen = 256
	chunk   = 32768
)

const (
	cmdOpen    = 0xbeef8011
	cmdClose   = 0xbeef8021
	cmdRead    = 0xbeef8031
	cmdWrite   = 0xbeef8041
	cmdRemove  = 0xbeef8071
	cmdMkdir   = 0xbeef8081
	cmdRmdir   = 0xbeef8091
	cmdDopen   = 0xbeef80a1
	cmdDclose  = 0xbeef80b1
	cmdDread   = 0xbeef80c1
	cmdSync    = 0xbeef8131
	cmdMount   = 0xbeef8141
	cmdUmount  = 0xbeef8151
	cmdDevlist = 0xbeef8f21
)

// Open flags.
const (
	ORead   = 0x0001
	OWrite  = 0x0002
	OCreate = 0x0200
	OTrunc  = 0x0400
)

// Stat mode bits (fileXio encoding).
const (
	ModeTypeMask = 0xf000
	ModeDir      = 0x1000
	ModeFile     = 0x2000
	ModeLink     = 0x4000
)

// Error is a negative result returned by the PS2.
type Error struct{ Errno int32 }

func (e *Error) Error() string {
	if s, ok := errnoText[e.Errno]; ok {
		return s
	}
	return "error " + strconv.Itoa(int(e.Errno))
}

var errnoText = map[int32]string{
	1:  "operation not permitted",
	2:  "no such file or directory",
	5:  "i/o error",
	9:  "bad file descriptor",
	13: "permission denied",
	16: "device busy",
	17: "file exists",
	19: "no such device",
	20: "not a directory",
	21: "is a directory",
	22: "invalid argument",
	24: "too many open files",
	28: "no space left on device",
	39: "directory not empty",
}

// DirEntry is one directory entry.
type DirEntry struct {
	Name     string
	Mode     uint32
	Attr     uint32
	Size     uint64
	Created  time.Time
	Accessed time.Time
	Modified time.Time
}

// IsDir reports whether the entry is a directory.
func (e DirEntry) IsDir() bool { return e.Mode&ModeTypeMask == ModeDir }

// Client talks to one ps2netfs connection.
type Client struct {
	conn    net.Conn
	Timeout time.Duration
}

// Dial connects to the ps2netfs server on host.
func Dial(host string, timeout time.Duration) (*Client, error) {
	d := net.Dialer{Timeout: timeout}
	c, err := d.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(Port)))
	if err != nil {
		return nil, err
	}
	return New(c, timeout), nil
}

// New wraps an established connection.
func New(conn net.Conn, timeout time.Duration) *Client {
	return &Client{conn: conn, Timeout: timeout}
}

// Close closes the connection.
func (c *Client) Close() error { return c.conn.Close() }

func (c *Client) arm() {
	if c.Timeout > 0 {
		c.conn.SetDeadline(time.Now().Add(c.Timeout))
	}
}

func (c *Client) send(cmd uint32, payload, data []byte) error {
	c.arm()
	pkt := make([]byte, hdrLen, hdrLen+len(payload)+len(data))
	binary.BigEndian.PutUint32(pkt, cmd)
	binary.BigEndian.PutUint16(pkt[4:], uint16(hdrLen+len(payload)))
	pkt = append(pkt, payload...)
	pkt = append(pkt, data...)
	_, err := c.conn.Write(pkt)
	return err
}

func (c *Client) recv(cmd uint32) ([]byte, error) {
	c.arm()
	var h [hdrLen]byte
	if _, err := io.ReadFull(c.conn, h[:]); err != nil {
		return nil, err
	}
	if got := binary.BigEndian.Uint32(h[:]); got != cmd+1 {
		return nil, fmt.Errorf("netfs: reply opcode %#x, want %#x", got, cmd+1)
	}
	n := int(binary.BigEndian.Uint16(h[4:]))
	if n < hdrLen+4 {
		return nil, fmt.Errorf("netfs: reply length %d too short", n)
	}
	body := make([]byte, n-hdrLen)
	if _, err := io.ReadFull(c.conn, body); err != nil {
		return nil, err
	}
	return body, nil
}

func result(body []byte) (int32, error) {
	r := int32(binary.BigEndian.Uint32(body))
	if r < 0 {
		return r, &Error{-r}
	}
	return r, nil
}

func (c *Client) call(cmd uint32, payload []byte) (int32, error) {
	if err := c.send(cmd, payload, nil); err != nil {
		return 0, err
	}
	body, err := c.recv(cmd)
	if err != nil {
		return 0, err
	}
	return result(body)
}

func pathField(s string) ([]byte, error) {
	if len(s) >= pathLen {
		return nil, fmt.Errorf("netfs: path longer than %d bytes", pathLen-1)
	}
	b := make([]byte, pathLen)
	copy(b, s)
	return b, nil
}

func i32(vs ...int32) []byte {
	b := make([]byte, 4*len(vs))
	for i, v := range vs {
		binary.BigEndian.PutUint32(b[4*i:], uint32(v))
	}
	return b
}

func (c *Client) pathCall(cmd uint32, flags int32, p string) (int32, error) {
	pf, err := pathField(p)
	if err != nil {
		return 0, err
	}
	return c.call(cmd, append(i32(flags), pf...))
}

// Open opens a file and returns its descriptor.
func (c *Client) Open(p string, flags int32) (int32, error) {
	return c.pathCall(cmdOpen, flags, p)
}

// CloseFile closes a descriptor from Open.
func (c *Client) CloseFile(fd int32) error {
	_, err := c.call(cmdClose, i32(fd))
	return err
}

// Read reads up to len(buf) bytes from fd.
func (c *Client) Read(fd int32, buf []byte) (int, error) {
	if len(buf) > chunk {
		buf = buf[:chunk]
	}
	if err := c.send(cmdRead, i32(fd, int32(len(buf))), nil); err != nil {
		return 0, err
	}
	body, err := c.recv(cmdRead)
	if err != nil {
		return 0, err
	}
	r, err := result(body)
	if err != nil {
		return 0, err
	}
	if len(body) < 8 {
		return 0, errors.New("netfs: short read reply")
	}
	n := int(binary.BigEndian.Uint32(body[4:]))
	if n != int(r) || n > len(buf) {
		return 0, fmt.Errorf("netfs: read reply claims %d bytes, result %d", n, r)
	}
	c.arm()
	if _, err := io.ReadFull(c.conn, buf[:n]); err != nil {
		return 0, err
	}
	return n, nil
}

// Write writes data to fd and returns the number of bytes accepted.
func (c *Client) Write(fd int32, data []byte) (int, error) {
	if len(data) > chunk {
		data = data[:chunk]
	}
	if err := c.send(cmdWrite, i32(fd, int32(len(data))), data); err != nil {
		return 0, err
	}
	body, err := c.recv(cmdWrite)
	if err != nil {
		return 0, err
	}
	r, err := result(body)
	return int(r), err
}

// Remove deletes a file.
func (c *Client) Remove(p string) error {
	_, err := c.pathCall(cmdRemove, 0, p)
	return err
}

// Mkdir creates a directory.
func (c *Client) Mkdir(p string) error {
	_, err := c.pathCall(cmdMkdir, 0, p)
	return err
}

// Rmdir removes an empty directory.
func (c *Client) Rmdir(p string) error {
	_, err := c.pathCall(cmdRmdir, 0, p)
	return err
}

// Sync flushes the device named by dev.
func (c *Client) Sync(dev string, flag int32) error {
	_, err := c.pathCall(cmdSync, flag, dev)
	return err
}

// Umount unmounts the file system on dev.
func (c *Client) Umount(dev string) error {
	_, err := c.pathCall(cmdUmount, 0, dev)
	return err
}

// Mount mounts the block device on the file system driver fsName.
func (c *Client) Mount(fsName, device string, flag int32, arg []byte) error {
	if len(arg) > pathLen {
		return fmt.Errorf("netfs: mount argument longer than %d bytes", pathLen)
	}
	fs, err := pathField(fsName)
	if err != nil {
		return err
	}
	dev, err := pathField(device)
	if err != nil {
		return err
	}
	a := make([]byte, pathLen)
	copy(a, arg)
	payload := append(fs, dev...)
	payload = append(payload, i32(flag)...)
	payload = append(payload, a...)
	payload = append(payload, i32(int32(len(arg)))...)
	_, err = c.call(cmdMount, payload)
	return err
}

// Devices lists the device names the PS2 exposes.
func (c *Client) Devices() ([]string, error) {
	const delim = ","
	pf, err := pathField(delim)
	if err != nil {
		return nil, err
	}
	if err := c.send(cmdDevlist, append(i32(0), pf...), nil); err != nil {
		return nil, err
	}
	body, err := c.recv(cmdDevlist)
	if err != nil {
		return nil, err
	}
	r, err := result(body)
	if err != nil {
		return nil, err
	}
	if len(body) < 8+pathLen {
		return nil, errors.New("netfs: short device list reply")
	}
	list := cstr(body[8 : 8+pathLen])
	if r == 0 || list == "" {
		return nil, nil
	}
	return strings.Split(list, delim), nil
}

func cstr(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}

func stamp(b []byte) time.Time {
	year := int(binary.LittleEndian.Uint16(b[6:]))
	if year == 0 || b[4] == 0 || b[5] == 0 {
		return time.Time{}
	}
	return time.Date(year, time.Month(b[5]), int(b[4]), int(b[3]), int(b[2]), int(b[1]), 0, time.UTC)
}

const direntLen = 4 + 4 + 4 + 4 + 24 + 4 + pathLen

// List returns the entries of a directory.
func (c *Client) List(p string) ([]DirEntry, error) {
	fd, err := c.pathCall(cmdDopen, 0, p)
	if err != nil {
		return nil, err
	}
	var out []DirEntry
	for {
		if err := c.send(cmdDread, i32(fd), nil); err != nil {
			return out, err
		}
		body, err := c.recv(cmdDread)
		if err != nil {
			return out, err
		}
		r, err := result(body)
		if err != nil {
			c.call(cmdDclose, i32(fd))
			return out, err
		}
		if r == 0 {
			break
		}
		if len(body) < direntLen {
			return out, errors.New("netfs: short directory reply")
		}
		var e DirEntry
		e.Mode = binary.BigEndian.Uint32(body[4:])
		e.Attr = binary.BigEndian.Uint32(body[8:])
		lo := binary.BigEndian.Uint32(body[12:])
		e.Created = stamp(body[16:24])
		e.Accessed = stamp(body[24:32])
		e.Modified = stamp(body[32:40])
		hi := binary.BigEndian.Uint32(body[40:])
		e.Size = uint64(hi)<<32 | uint64(lo)
		e.Name = cstr(body[44 : 44+pathLen])
		out = append(out, e)
	}
	_, err = c.call(cmdDclose, i32(fd))
	return out, err
}

// Download copies a remote file to w.
func (c *Client) Download(remote string, w io.Writer) (int64, error) {
	fd, err := c.Open(remote, ORead)
	if err != nil {
		return 0, err
	}
	var total int64
	buf := make([]byte, chunk)
	for {
		n, err := c.Read(fd, buf)
		if err != nil {
			c.CloseFile(fd)
			return total, err
		}
		if n == 0 {
			break
		}
		if _, err := w.Write(buf[:n]); err != nil {
			c.CloseFile(fd)
			return total, err
		}
		total += int64(n)
	}
	return total, c.CloseFile(fd)
}

// Upload copies r to a remote file, replacing it.
func (c *Client) Upload(remote string, r io.Reader) (int64, error) {
	fd, err := c.Open(remote, OWrite|OCreate|OTrunc)
	if err != nil {
		return 0, err
	}
	var total int64
	buf := make([]byte, chunk)
	for {
		n, rerr := io.ReadFull(r, buf)
		for off := 0; off < n; {
			w, err := c.Write(fd, buf[off:n])
			if err != nil {
				c.CloseFile(fd)
				return total, err
			}
			if w <= 0 {
				c.CloseFile(fd)
				return total, errors.New("netfs: write accepted no data")
			}
			off += w
			total += int64(w)
		}
		if rerr == io.EOF || rerr == io.ErrUnexpectedEOF {
			break
		}
		if rerr != nil {
			c.CloseFile(fd)
			return total, rerr
		}
	}
	return total, c.CloseFile(fd)
}
