// Package cli implements the ps2client command line.
package cli

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/ps2build/tools/pkg/ps2client/hostfs"
	"github.com/ps2build/tools/pkg/ps2client/link"
	"github.com/ps2build/tools/pkg/ps2client/proto"
)

const defaultHost = "192.168.0.10"

type options struct {
	host        string
	timeout     int
	root        string
	readOnly    bool
	verbose     bool
	commandPort int
	requestPort int
	logPort     int
}

// Execute runs the command line.
func Execute() error {
	return newRoot().Execute()
}

func newRoot() *cobra.Command {
	o := &options{}
	root := &cobra.Command{
		Use:   "ps2client [-h hostname] [-t seconds] <command> [arguments]",
		Short: "Control a PS2 running ps2link",
		Long: `ps2client sends commands to a PS2 running ps2link, serves the files the PS2
reads and writes through host:, and prints its console output.

The PS2's address comes from -h, then $PS2HOSTNAME, then ` + defaultHost + `.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	// cobra claims -h for help; this tool uses it for the host name.
	root.PersistentFlags().Bool("help", false, "show help")
	root.PersistentFlags().StringVarP(&o.host, "hostname", "h", "", "address of the PS2 (default $PS2HOSTNAME, else "+defaultHost+")")
	root.PersistentFlags().IntVarP(&o.timeout, "timeout", "t", -1, "seconds without activity before exiting; 0 waits forever")
	root.PersistentFlags().StringVar(&o.root, "root", ".", "directory served as host:")
	root.PersistentFlags().BoolVar(&o.readOnly, "read-only", false, "refuse writes, creates and deletes from the PS2")
	root.PersistentFlags().BoolVarP(&o.verbose, "verbose", "v", false, "log each file request")
	root.PersistentFlags().IntVar(&o.commandPort, "command-port", 0, "override the ps2link command port")
	root.PersistentFlags().IntVar(&o.requestPort, "request-port", 0, "override the ps2link request port")
	root.PersistentFlags().IntVar(&o.logPort, "log-port", 0, "override the console text port")
	for _, f := range []string{"command-port", "request-port", "log-port"} {
		root.PersistentFlags().MarkHidden(f)
	}

	add := func(c *cobra.Command) { root.AddCommand(c) }

	add(&cobra.Command{
		Use: "reset", Short: "Reset ps2link", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error { return o.send(proto.Simple(proto.CmdReset)) },
	})
	add(&cobra.Command{
		Use: "poweroff", Short: "Power the PS2 off", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error { return o.send(proto.Simple(proto.CmdPowerOff)) },
	})
	add(&cobra.Command{
		Use: "scrdump", Short: "Show exceptions on the PS2's screen", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error { return o.send(proto.Simple(proto.CmdScrDump)) },
	})
	add(&cobra.Command{
		Use: "netdump", Short: "Send exceptions to the network console", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error { return o.send(proto.Simple(proto.CmdNetDump)) },
	})
	add(&cobra.Command{
		Use: "execiop <file> [arguments]", Short: "Load and start an IRX module on the IOP",
		Args: cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, a []string) error {
			pkt, err := proto.Exec(proto.CmdExecIOP, a[0], a[1:])
			if err != nil {
				return err
			}
			return o.serve(pkt, 3*time.Second)
		},
	})
	add(&cobra.Command{
		Use: "execee <file> [arguments]", Short: "Load and start an ELF on the EE and serve its host: files",
		Args: cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, a []string) error {
			pkt, err := proto.Exec(proto.CmdExecEE, a[0], a[1:])
			if err != nil {
				return err
			}
			return o.serve(pkt, 0)
		},
	})
	add(&cobra.Command{
		Use: "dumpmem <offset> <size> <file>", Short: "Write PS2 memory to a file",
		Args: cobra.ExactArgs(3),
		RunE: func(_ *cobra.Command, a []string) error { return o.mem(proto.CmdDumpMem, a) },
	})
	add(&cobra.Command{
		Use: "writemem <offset> <size> <file>", Short: "Read a file into PS2 memory",
		Args: cobra.ExactArgs(3),
		RunE: func(_ *cobra.Command, a []string) error { return o.mem(proto.CmdWriteMem, a) },
	})
	add(&cobra.Command{
		Use: "startvu <0|1>", Short: "Start a vector unit", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, a []string) error { return o.vu(proto.CmdStartVU, a[0]) },
	})
	add(&cobra.Command{
		Use: "stopvu <0|1>", Short: "Stop a vector unit", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, a []string) error { return o.vu(proto.CmdStopVU, a[0]) },
	})
	add(&cobra.Command{
		Use:   "dumpreg <group> <file>",
		Short: "Write a register group to a file",
		Long:  "Groups: " + strings.Join(groupNames(), ", ") + " (or a number).",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, a []string) error {
			g, err := parseGroup(a[0])
			if err != nil {
				return err
			}
			pkt, err := proto.DumpReg(g, a[1])
			if err != nil {
				return err
			}
			return o.serve(pkt, 3*time.Second)
		},
	})
	add(&cobra.Command{
		Use: "gsexec <size> <file>", Short: "Send a file to the GS", Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, a []string) error {
			n, err := strconv.ParseUint(a[0], 0, 16)
			if err != nil {
				return fmt.Errorf("size: %w", err)
			}
			pkt, err := proto.GSExec(uint16(n), a[1])
			if err != nil {
				return err
			}
			return o.serve(pkt, 3*time.Second)
		},
	})
	add(&cobra.Command{
		Use: "listen", Short: "Print the PS2's console output", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error { return o.listen() },
	})
	add(o.fsCommand())
	return root
}

func groupNames() []string {
	order := []string{"dma", "intc", "timer", "gs", "sif", "fifo", "gif", "vif0", "vif1", "ipu", "all", "vu0", "vu1"}
	return order
}

func parseGroup(s string) (int32, error) {
	if g, ok := proto.RegisterGroups[strings.ToLower(s)]; ok {
		return g, nil
	}
	n, err := strconv.ParseInt(s, 0, 32)
	if err != nil || n < 0 || n > proto.RegVU1 {
		return 0, fmt.Errorf("unknown register group %q", s)
	}
	return int32(n), nil
}

func (o *options) hostName() string {
	if o.host != "" {
		return o.host
	}
	if h := os.Getenv("PS2HOSTNAME"); h != "" {
		return h
	}
	return defaultHost
}

func (o *options) client() *link.Client {
	return &link.Client{Host: o.hostName(), CommandPort: o.commandPort, RequestPort: o.requestPort, LogPort: o.logPort}
}

func (o *options) send(pkt []byte) error {
	return o.client().SendCommand(pkt)
}

func (o *options) mem(cmd uint32, a []string) error {
	off, err := strconv.ParseUint(a[0], 0, 32)
	if err != nil {
		return fmt.Errorf("offset: %w", err)
	}
	size, err := strconv.ParseUint(a[1], 0, 32)
	if err != nil {
		return fmt.Errorf("size: %w", err)
	}
	pkt, err := proto.Mem(cmd, uint32(off), uint32(size), a[2])
	if err != nil {
		return err
	}
	return o.serve(pkt, 3*time.Second)
}

func (o *options) vu(cmd uint32, s string) error {
	n, err := strconv.ParseInt(s, 0, 32)
	if err != nil || (n != 0 && n != 1) {
		return fmt.Errorf("vector unit must be 0 or 1, got %q", s)
	}
	return o.send(proto.VU(cmd, int32(n)))
}

func (o *options) idle(def time.Duration) time.Duration {
	if o.timeout < 0 {
		return def
	}
	return time.Duration(o.timeout) * time.Second
}

func interruptContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt)
}

// serve starts the file server, sends the command, and answers the PS2's file
// requests until the idle time passes without one.
func (o *options) serve(pkt []byte, defIdle time.Duration) error {
	srv, err := hostfs.New(o.root, o.readOnly)
	if err != nil {
		return fmt.Errorf("host root: %w", err)
	}
	defer srv.Close()
	if o.verbose {
		srv.Log = func(f string, a ...any) { fmt.Fprintf(os.Stderr, "ps2client: "+f+"\n", a...) }
	}

	ctx, stop := interruptContext()
	defer stop()

	c := o.client()
	// Console text does not stop the command when it cannot be received,
	// for example because another program already holds the port.
	go func() {
		if err := c.ListenLog(ctx, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "ps2client: console text unavailable: %v\n", err)
		}
	}()

	sess, err := c.Connect(ctx, srv, 10*time.Second)
	if err != nil {
		return err
	}
	defer sess.Close()

	o.logf("connected to %s, sending command 0x%08X", c.Host, binary.BigEndian.Uint32(pkt))
	if err := c.SendCommand(pkt); err != nil {
		return err
	}
	start := time.Now()
	err = sess.Serve(ctx, o.idle(defIdle))
	o.logf("session ended after %s (%v)", time.Since(start).Round(time.Millisecond), err)
	return err
}

func (o *options) logf(format string, args ...any) {
	if o.verbose {
		fmt.Fprintf(os.Stderr, "ps2client: "+format+"\n", args...)
	}
}

// idleWriter forwards to w and reports activity.
type idleWriter struct {
	w    io.Writer
	ping func()
}

func (i idleWriter) Write(p []byte) (int, error) {
	i.ping()
	return i.w.Write(p)
}

func (o *options) listen() error {
	ctx, stop := interruptContext()
	defer stop()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		mu    sync.Mutex
		timer *time.Timer
	)
	if d := o.idle(0); d > 0 {
		timer = time.AfterFunc(d, cancel)
	}
	ping := func() {
		mu.Lock()
		defer mu.Unlock()
		if timer != nil {
			timer.Reset(o.idle(0))
		}
	}
	return o.client().ListenLog(ctx, idleWriter{w: os.Stdout, ping: ping})
}
