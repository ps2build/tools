// Package proto encodes and decodes the packets ps2link exchanges with a PC.
// See cmd/ps2client/docs/protocol.md for the wire description.
package proto

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Ports on which ps2link listens or expects the PC.
const (
	RequestPort = 0x4711 // TCP on the PS2: the PC connects, the PS2 sends file requests
	CommandPort = 0x4712 // UDP on the PS2: commands from the PC
	LogPort     = 0x4712 // UDP on the PC: console text from the PS2
)

// PathSize is the size of every fixed path field in a packet.
const PathSize = 256

// HeaderSize is the size of the command and length prefix of every packet.
const HeaderSize = 6

// Command opcodes (PC to PS2, UDP).
const (
	CmdReset    uint32 = 0xBABE0201
	CmdExecIOP  uint32 = 0xBABE0202
	CmdExecEE   uint32 = 0xBABE0203
	CmdPowerOff uint32 = 0xBABE0204
	CmdScrDump  uint32 = 0xBABE0205
	CmdNetDump  uint32 = 0xBABE0206
	CmdDumpMem  uint32 = 0xBABE0207
	CmdStartVU  uint32 = 0xBABE0208
	CmdStopVU   uint32 = 0xBABE0209
	CmdDumpReg  uint32 = 0xBABE020A
	CmdGSExec   uint32 = 0xBABE020B
	CmdWriteMem uint32 = 0xBABE020C
)

// File request opcodes (PS2 to PC, TCP). The reply uses the opcode plus one.
const (
	ReqOpen     uint32 = 0xBABE0111
	ReqClose    uint32 = 0xBABE0121
	ReqRead     uint32 = 0xBABE0131
	ReqWrite    uint32 = 0xBABE0141
	ReqLseek    uint32 = 0xBABE0151
	ReqOpenDir  uint32 = 0xBABE0161
	ReqCloseDir uint32 = 0xBABE0171
	ReqReadDir  uint32 = 0xBABE0181
	ReqRemove   uint32 = 0xBABE0191
	ReqMkdir    uint32 = 0xBABE01A1
	ReqRmdir    uint32 = 0xBABE01B1
	ReqGetStat  uint32 = 0xBABE01C1
)

// Register groups accepted by the dump register command.
const (
	RegDMA   = 0
	RegINTC  = 1
	RegTimer = 2
	RegGS    = 3
	RegSIF   = 4
	RegFIFO  = 5
	RegGIF   = 6
	RegVIF0  = 7
	RegVIF1  = 8
	RegIPU   = 9
	RegAll   = 10
	RegVU0   = 11
	RegVU1   = 12
)

// RegisterGroups maps the names the command line accepts to group numbers.
var RegisterGroups = map[string]int32{
	"dma": RegDMA, "intc": RegINTC, "timer": RegTimer, "gs": RegGS, "sif": RegSIF,
	"fifo": RegFIFO, "gif": RegGIF, "vif0": RegVIF0, "vif1": RegVIF1, "ipu": RegIPU,
	"all": RegAll, "vu0": RegVU0, "vu1": RegVU1,
}

// Reply sizes, in bytes, as announced in the length field.
const (
	resultReplySize  = HeaderSize + 4
	readReplySize    = HeaderSize + 8
	direntReplySize  = HeaderSize + 4 + 4*3 + 8*3 + 4 + 256
	getStatReplySize = HeaderSize + 4 + 4*3 + 8*3 + 4
)

// MaxRequestSize bounds the length field accepted from the PS2.
const MaxRequestSize = 4096

func putHeader(b []byte, cmd uint32, size int) {
	binary.BigEndian.PutUint32(b[0:], cmd)
	binary.BigEndian.PutUint16(b[4:], uint16(size))
}

func putPath(b []byte, s string) error {
	if len(b) != PathSize {
		return errors.New("internal: bad path field size")
	}
	if strings.IndexByte(s, 0) >= 0 {
		return errors.New("path contains a NUL byte")
	}
	if len(s) >= PathSize {
		return fmt.Errorf("path is %d bytes, the limit is %d", len(s), PathSize-1)
	}
	copy(b, s)
	return nil
}

func packet(cmd uint32, payload int) []byte {
	b := make([]byte, HeaderSize+payload)
	putHeader(b, cmd, len(b))
	return b
}

// Simple returns a command with no payload (reset, poweroff, scrdump, netdump).
func Simple(cmd uint32) []byte { return packet(cmd, 0) }

// Exec builds an execiop or execee command. The path and arguments are packed
// into one NUL-separated field; argc counts the path.
func Exec(cmd uint32, path string, args []string) ([]byte, error) {
	var packed []byte
	for _, s := range append([]string{path}, args...) {
		if strings.IndexByte(s, 0) >= 0 {
			return nil, errors.New("argument contains a NUL byte")
		}
		packed = append(packed, s...)
		packed = append(packed, 0)
	}
	if len(packed) > PathSize-1 {
		return nil, fmt.Errorf("path and arguments take %d bytes, the limit is %d", len(packed), PathSize-1)
	}
	b := packet(cmd, 4+PathSize)
	binary.BigEndian.PutUint32(b[HeaderSize:], uint32(1+len(args)))
	copy(b[HeaderSize+4:], packed)
	return b, nil
}

// VU builds a startvu or stopvu command.
func VU(cmd uint32, unit int32) []byte {
	b := packet(cmd, 4)
	binary.BigEndian.PutUint32(b[HeaderSize:], uint32(unit))
	return b
}

// Mem builds a dumpmem or writemem command.
func Mem(cmd uint32, offset, size uint32, path string) ([]byte, error) {
	b := packet(cmd, 8+PathSize)
	binary.BigEndian.PutUint32(b[HeaderSize:], offset)
	binary.BigEndian.PutUint32(b[HeaderSize+4:], size)
	if err := putPath(b[HeaderSize+8:], path); err != nil {
		return nil, err
	}
	return b, nil
}

// DumpReg builds a dump registers command.
func DumpReg(group int32, path string) ([]byte, error) {
	b := packet(CmdDumpReg, 4+PathSize)
	binary.BigEndian.PutUint32(b[HeaderSize:], uint32(group))
	if err := putPath(b[HeaderSize+4:], path); err != nil {
		return nil, err
	}
	return b, nil
}

// GSExec builds a command that sends a file to the GS.
func GSExec(size uint16, path string) ([]byte, error) {
	b := packet(CmdGSExec, 2+PathSize)
	binary.BigEndian.PutUint16(b[HeaderSize:], size)
	if err := putPath(b[HeaderSize+2:], path); err != nil {
		return nil, err
	}
	return b, nil
}

// Request is a decoded file request from the PS2.
type Request struct {
	Op     uint32
	Flags  int32  // open
	Path   string // open, opendir, remove, mkdir, rmdir, getstat
	Fd     int32  // close, read, write, lseek, closedir, readdir
	N      int32  // read: bytes wanted; write: bytes in Data
	Offset int32  // lseek
	Whence int32  // lseek
	Mode   int32  // mkdir
	Data   []byte // write
}

func cstring(b []byte) string {
	if i := strings.IndexByte(string(b), 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}

// ReadRequest reads one request from r.
func ReadRequest(r io.Reader) (*Request, error) {
	var hdr [HeaderSize]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	op := binary.BigEndian.Uint32(hdr[0:])
	size := int(binary.BigEndian.Uint16(hdr[4:]))
	if size < HeaderSize || size > MaxRequestSize {
		return nil, fmt.Errorf("request 0x%08X has bad length %d", op, size)
	}
	body := make([]byte, size-HeaderSize)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	req := &Request{Op: op}
	i32 := func(off int) int32 { return int32(binary.BigEndian.Uint32(body[off:])) }
	need := func(n int) error {
		if len(body) < n {
			return fmt.Errorf("request 0x%08X is too short (%d bytes of payload, need %d)", op, len(body), n)
		}
		return nil
	}
	switch op {
	case ReqOpen, ReqOpenDir:
		if err := need(4 + PathSize); err != nil {
			return nil, err
		}
		req.Flags = i32(0)
		req.Path = cstring(body[4 : 4+PathSize])
	case ReqClose, ReqCloseDir, ReqReadDir:
		if err := need(4); err != nil {
			return nil, err
		}
		req.Fd = i32(0)
	case ReqRead:
		if err := need(8); err != nil {
			return nil, err
		}
		req.Fd, req.N = i32(0), i32(4)
	case ReqWrite:
		if err := need(8); err != nil {
			return nil, err
		}
		req.Fd, req.N = i32(0), i32(4)
		if req.N < 0 || req.N > MaxRequestSize {
			return nil, fmt.Errorf("write request announces %d bytes", req.N)
		}
		req.Data = make([]byte, req.N)
		if _, err := io.ReadFull(r, req.Data); err != nil {
			return nil, err
		}
	case ReqLseek:
		if err := need(12); err != nil {
			return nil, err
		}
		req.Fd, req.Offset, req.Whence = i32(0), i32(4), i32(8)
	case ReqRemove, ReqRmdir, ReqGetStat:
		if err := need(PathSize); err != nil {
			return nil, err
		}
		req.Path = cstring(body[:PathSize])
	case ReqMkdir:
		if err := need(4 + PathSize); err != nil {
			return nil, err
		}
		req.Mode = int32(binary.LittleEndian.Uint32(body[0:]))
		req.Path = cstring(body[4 : 4+PathSize])
	}
	return req, nil
}

// ReplyResult builds a reply that carries only a result code.
func ReplyResult(req uint32, result int32) []byte {
	b := make([]byte, resultReplySize)
	putHeader(b, req+1, resultReplySize)
	binary.BigEndian.PutUint32(b[HeaderSize:], uint32(result))
	return b
}

// ReplyRead builds a read reply. On error result is negative and data is empty;
// the byte count field then carries the error too so the PS2 sees a failure.
func ReplyRead(result int32, data []byte) []byte {
	b := make([]byte, readReplySize+len(data))
	putHeader(b, ReqRead+1, readReplySize)
	binary.BigEndian.PutUint32(b[HeaderSize:], uint32(result))
	n := int32(len(data))
	if result < 0 {
		n = result
	}
	binary.BigEndian.PutUint32(b[HeaderSize+4:], uint32(n))
	copy(b[readReplySize:], data)
	return b
}

// Stat is the file information sent in getstat and readdir replies.
type Stat struct {
	Mode  uint32
	Attr  uint32
	Size  uint64
	Ctime [8]byte
	Atime [8]byte
	Mtime [8]byte
}

func putStat(b []byte, s Stat) {
	binary.BigEndian.PutUint32(b[0:], s.Mode)
	binary.BigEndian.PutUint32(b[4:], s.Attr)
	binary.BigEndian.PutUint32(b[8:], uint32(s.Size))
	copy(b[12:], s.Ctime[:])
	copy(b[20:], s.Atime[:])
	copy(b[28:], s.Mtime[:])
	binary.BigEndian.PutUint32(b[36:], uint32(s.Size>>32))
}

// ReplyGetStat builds a getstat reply. On error result is negative and s is ignored.
func ReplyGetStat(result int32, s Stat) []byte {
	b := make([]byte, getStatReplySize)
	putHeader(b, ReqGetStat+1, getStatReplySize)
	binary.BigEndian.PutUint32(b[HeaderSize:], uint32(result))
	if result >= 0 {
		putStat(b[HeaderSize+4:], s)
	}
	return b
}

// ReplyReadDir builds a readdir reply. A result of 0 marks the end of the directory.
func ReplyReadDir(result int32, s Stat, name string) []byte {
	b := make([]byte, direntReplySize)
	putHeader(b, ReqReadDir+1, direntReplySize)
	binary.BigEndian.PutUint32(b[HeaderSize:], uint32(result))
	if result > 0 {
		putStat(b[HeaderSize+4:], s)
		n := name
		if len(n) > 255 {
			n = n[:255]
		}
		copy(b[HeaderSize+4+40:], n)
	}
	return b
}
