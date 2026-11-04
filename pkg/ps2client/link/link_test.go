package link

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ps2build/tools/pkg/ps2client/hostfs"
	"github.com/ps2build/tools/pkg/ps2client/proto"
)

func freeUDPPort(t *testing.T) int {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	return pc.LocalAddr().(*net.UDPAddr).Port
}

func TestSendCommandDeliversOneDatagram(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	c := &Client{Host: "127.0.0.1", CommandPort: pc.LocalAddr().(*net.UDPAddr).Port}

	want, _ := proto.Mem(proto.CmdDumpMem, 0x100000, 16, "out.bin")
	if err := c.SendCommand(want); err != nil {
		t.Fatal(err)
	}
	pc.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 2048)
	n, _, err := pc.ReadFrom(buf)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf[:n], want) {
		t.Errorf("datagram differs from the command that was sent")
	}
}

func TestListenLogPrintsDatagrams(t *testing.T) {
	port := freeUDPPort(t)
	c := &Client{Host: "127.0.0.1", LogPort: port}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var got strings.Builder
	w := writerFunc(func(p []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		return got.Write(p)
	})
	done := make(chan error, 1)
	go func() { done <- c.ListenLog(ctx, w) }()

	conn, err := net.Dial("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn.Write([]byte("hello from the ps2\n"))
		time.Sleep(50 * time.Millisecond)
		mu.Lock()
		ok := strings.Contains(got.String(), "hello from the ps2")
		mu.Unlock()
		if ok {
			break
		}
	}
	mu.Lock()
	if !strings.Contains(got.String(), "hello from the ps2") {
		t.Errorf("console text never arrived, got %q", got.String())
	}
	mu.Unlock()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("ListenLog returned %v after cancel", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("ListenLog did not stop after cancel")
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func reqPacket(op uint32, payload []byte) []byte {
	b := make([]byte, proto.HeaderSize+len(payload))
	binary.BigEndian.PutUint32(b, op)
	binary.BigEndian.PutUint16(b[4:], uint16(len(b)))
	copy(b[proto.HeaderSize:], payload)
	return b
}

func TestSessionAnswersFileRequests(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644)
	srv, err := hostfs.New(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		open := make([]byte, 4+proto.PathSize)
		binary.BigEndian.PutUint32(open, 1)
		copy(open[4:], "a.txt")
		conn.Write(reqPacket(proto.ReqOpen, open))
		reply := make([]byte, 10)
		if _, err := readFull(conn, reply); err != nil {
			return
		}
		fd := binary.BigEndian.Uint32(reply[6:])
		read := make([]byte, 8)
		binary.BigEndian.PutUint32(read, fd)
		binary.BigEndian.PutUint32(read[4:], 100)
		conn.Write(reqPacket(proto.ReqRead, read))
		rr := make([]byte, 14+5)
		readFull(conn, rr)
		results <- string(rr[14:])
	}()

	c := &Client{Host: "127.0.0.1", RequestPort: port}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sess, err := c.Connect(ctx, srv, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	if err := sess.Serve(ctx, time.Second); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	select {
	case got := <-results:
		if got != "hello" {
			t.Errorf("PS2 read %q, want hello", got)
		}
	case <-time.After(2 * time.Second):
		t.Error("the fake PS2 never received the file contents")
	}
}

var results = make(chan string, 1)

func readFull(c net.Conn, b []byte) (int, error) {
	total := 0
	for total < len(b) {
		n, err := c.Read(b[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func TestServeStopsWhenIdle(t *testing.T) {
	srv, _ := hostfs.New(t.TempDir(), false)
	defer srv.Close()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			time.Sleep(3 * time.Second)
			conn.Close()
		}
	}()
	c := &Client{Host: "127.0.0.1", RequestPort: ln.Addr().(*net.TCPAddr).Port}
	sess, err := c.Connect(context.Background(), srv, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	start := time.Now()
	if err := sess.Serve(context.Background(), 200*time.Millisecond); err != nil {
		t.Fatalf("idle timeout should not be an error: %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("Serve took %s to notice it was idle", time.Since(start))
	}
}

func TestConnectGivesUpAfterTimeout(t *testing.T) {
	srv, _ := hostfs.New(t.TempDir(), false)
	defer srv.Close()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	c := &Client{Host: "127.0.0.1", RequestPort: port}
	start := time.Now()
	if _, err := c.Connect(context.Background(), srv, 700*time.Millisecond); err == nil {
		t.Fatal("connect to a closed port succeeded")
	}
	if time.Since(start) > 4*time.Second {
		t.Errorf("Connect kept retrying for %s", time.Since(start))
	}
}
