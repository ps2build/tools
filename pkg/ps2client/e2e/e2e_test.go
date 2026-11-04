//go:build e2e

// Package e2e runs the client against a real ps2link, normally inside PCSX2.
// See cmd/ps2client/docs/testing.md for the setup. Run with:
//
//	PS2CLIENT_E2E_HOST=169.254.250.2 PS2CLIENT_E2E_HOSTTEST=path/to/hosttest.elf \
//	PS2CLIENT_E2E_DEVICE=pkfs: go test -tags e2e -v -count=1 ./pkg/ps2client/e2e
//
// Each test resets ps2link first, so it must be reachable and idle.
package e2e

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ps2build/tools/pkg/ps2client/hostfs"
	"github.com/ps2build/tools/pkg/ps2client/link"
	"github.com/ps2build/tools/pkg/ps2client/proto"
)

type env struct {
	client *link.Client
	device string
	elf    string
}

func setup(t *testing.T) env {
	t.Helper()
	host := os.Getenv("PS2CLIENT_E2E_HOST")
	if host == "" {
		t.Skip("PS2CLIENT_E2E_HOST is not set")
	}
	dev := os.Getenv("PS2CLIENT_E2E_DEVICE")
	if dev == "" {
		dev = "host:"
	}
	c := &link.Client{Host: host}
	if err := c.SendCommand(proto.Simple(proto.CmdReset)); err != nil {
		t.Fatal(err)
	}
	waitForPS2Link(t, c)
	return env{client: c, device: dev, elf: os.Getenv("PS2CLIENT_E2E_HOSTTEST")}
}

// waitForPS2Link waits for the reset to take effect and for ps2link to accept
// requests again.
func waitForPS2Link(t *testing.T, c *link.Client) {
	t.Helper()
	time.Sleep(8 * time.Second)
	srv, err := hostfs.New(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	sess, err := c.Connect(ctx, srv, 80*time.Second)
	if err != nil {
		t.Fatalf("ps2link did not come back after reset: %v", err)
	}
	sess.Close()
	time.Sleep(3 * time.Second)
}

// run serves root while the command runs, stopping after the PS2 has been quiet
// for idle or until done reports true.
func (e env) run(t *testing.T, root string, cmd []byte, idle time.Duration, done func() bool) {
	t.Helper()
	srv, err := hostfs.New(root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	sess, err := e.client.Connect(ctx, srv, 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	if err := e.client.SendCommand(cmd); err != nil {
		t.Fatal(err)
	}
	if done != nil {
		go func() {
			for ctx.Err() == nil {
				if done() {
					cancel()
					return
				}
				time.Sleep(500 * time.Millisecond)
			}
		}()
	}
	if err := sess.Serve(ctx, idle); err != nil {
		t.Fatalf("serving the PS2: %v", err)
	}
}

func pattern(offset, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte((offset+i)*31 + 7)
	}
	return b
}

func TestFileDeviceThroughPS2Link(t *testing.T) {
	e := setup(t)
	if e.elf == "" {
		t.Skip("PS2CLIENT_E2E_HOSTTEST is not set")
	}
	root := t.TempDir()
	elf, err := os.ReadFile(e.elf)
	if err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(root, "hosttest.elf"), elf, 0o644))
	must(os.WriteFile(filepath.Join(root, "in.txt"), []byte("PC to PS2 works\n"), 0o644))
	must(os.WriteFile(filepath.Join(root, "big.bin"), pattern(0, 300*1024), 0o644))
	must(os.Mkdir(filepath.Join(root, "sub"), 0o755))

	cmd, err := proto.Exec(proto.CmdExecEE, e.device+"hosttest.elf", nil)
	must(err)
	results := filepath.Join(root, "results.txt")

	var console syncBuffer
	logCtx, stopLog := context.WithCancel(context.Background())
	logDone := make(chan error, 1)
	go func() { logDone <- e.client.ListenLog(logCtx, &console) }()

	e.run(t, root, cmd, 30*time.Second, func() bool {
		b, err := os.ReadFile(results)
		return err == nil && bytes.Contains(b, []byte("HT: DONE"))
	})
	time.Sleep(time.Second)
	stopLog()
	if err := <-logDone; err != nil {
		t.Errorf("console listener: %v", err)
	}
	for _, want := range []string{"HT: BEGIN", "HT: PASS read_text", "HT: DONE passed="} {
		if !strings.Contains(console.String(), want) {
			t.Errorf("console text from the PS2 lacks %q; got %d bytes", want, len(console.String()))
		}
	}

	report, err := os.ReadFile(results)
	if err != nil {
		t.Fatalf("the PS2 program never wrote its report: %v", err)
	}
	for _, line := range strings.Split(string(report), "\n") {
		if strings.HasPrefix(line, "HT: FAIL") {
			t.Errorf("PS2 reported: %s", line)
		}
	}
	if !strings.Contains(string(report), "failed=0") {
		t.Errorf("report does not end with failed=0:\n%s", report)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "out.txt")); string(got) != "PS2 to PC works\n" {
		t.Errorf("out.txt holds %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "big2.bin")); !bytes.Equal(got, pattern(5, 100*1024)) {
		t.Errorf("big2.bin is %d bytes and differs from what the PS2 wrote", len(got))
	}
}

func TestMemoryRoundTrip(t *testing.T) {
	e := setup(t)
	root := t.TempDir()
	data := pattern(3, 64)
	if err := os.WriteFile(filepath.Join(root, "in.bin"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	wr, _ := proto.Mem(proto.CmdWriteMem, 0x300000, 64, e.device+"in.bin")
	e.run(t, root, wr, 4*time.Second, nil)
	dump, _ := proto.Mem(proto.CmdDumpMem, 0x300000, 64, e.device+"out.bin")
	e.run(t, root, dump, 4*time.Second, nil)

	got, err := os.ReadFile(filepath.Join(root, "out.bin"))
	if err != nil {
		t.Fatalf("dumpmem produced no file: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("memory read back differs: %x", got)
	}
}

func TestRegisterDumpSizes(t *testing.T) {
	e := setup(t)
	root := t.TempDir()
	// The FIFO group is left out: PCSX2 asserts when the VIF FIFO is read.
	want := map[string]int{"dma": 208, "intc": 8, "timer": 56, "gs": 8, "sif": 4, "gif": 32, "vif0": 76, "vif1": 92, "ipu": 16}
	for name, size := range want {
		cmd, err := proto.DumpReg(proto.RegisterGroups[name], e.device+name+".bin")
		if err != nil {
			t.Fatal(err)
		}
		e.run(t, root, cmd, 3*time.Second, nil)
		st, err := os.Stat(filepath.Join(root, name+".bin"))
		if err != nil {
			t.Errorf("%s: no file written: %v", name, err)
			continue
		}
		if int(st.Size()) != size {
			t.Errorf("%s: %d bytes, want %d", name, st.Size(), size)
		}
	}
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
