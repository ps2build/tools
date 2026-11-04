// Package link connects to a PS2 running ps2link: it sends commands over UDP,
// serves the PS2's file requests over TCP and prints its console text.
package link

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"

	"github.com/ps2build/tools/pkg/ps2client/hostfs"
	"github.com/ps2build/tools/pkg/ps2client/proto"
)

// Client addresses one PS2.
type Client struct {
	Host string
	// Ports default to the ps2link values when zero.
	CommandPort int
	RequestPort int
	LogPort     int
}

func (c *Client) port(v, def int) int {
	if v != 0 {
		return v
	}
	return def
}

func (c *Client) addr(p int) string {
	return net.JoinHostPort(c.Host, strconv.Itoa(p))
}

// SendCommand sends one command datagram.
func (c *Client) SendCommand(pkt []byte) error {
	conn, err := net.Dial("udp", c.addr(c.port(c.CommandPort, proto.CommandPort)))
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.Write(pkt)
	return err
}

// Session is an open file-request connection to the PS2.
type Session struct {
	conn net.Conn
	srv  *hostfs.Server
}

// Connect opens the request connection, retrying until timeout because ps2link
// may still be starting.
func (c *Client) Connect(ctx context.Context, srv *hostfs.Server, timeout time.Duration) (*Session, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		d := net.Dialer{Timeout: 2 * time.Second}
		conn, err := d.DialContext(ctx, "tcp", c.addr(c.port(c.RequestPort, proto.RequestPort)))
		if err == nil {
			if tc, ok := conn.(*net.TCPConn); ok {
				tc.SetNoDelay(true)
			}
			return &Session{conn: conn, srv: srv}, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("connecting to ps2link on %s: %w", c.Host, lastErr)
		}
		select {
		case <-time.After(500 * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// Close ends the session.
func (s *Session) Close() error { return s.conn.Close() }

// Serve answers the PS2's requests until ctx ends, the PS2 disconnects, or no
// request arrives for idle (when idle is not zero). A disconnect or idle
// timeout is not an error.
func (s *Session) Serve(ctx context.Context, idle time.Duration) error {
	go func() {
		<-ctx.Done()
		s.conn.SetDeadline(time.Now())
	}()
	for {
		if idle > 0 {
			s.conn.SetReadDeadline(time.Now().Add(idle))
		} else {
			s.conn.SetReadDeadline(time.Time{})
		}
		req, err := proto.ReadRequest(s.conn)
		if err != nil {
			var ne net.Error
			switch {
			case ctx.Err() != nil:
				return nil
			case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
				return nil
			case errors.As(err, &ne) && ne.Timeout():
				return nil
			}
			return err
		}
		reply := s.srv.Handle(req)
		s.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if _, err := s.conn.Write(reply); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
	}
}

// ListenLog prints console text sent by the PS2 to w until ctx ends.
func (c *Client) ListenLog(ctx context.Context, w io.Writer) error {
	pc, err := net.ListenPacket("udp", ":"+strconv.Itoa(c.port(c.LogPort, proto.LogPort)))
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		pc.Close()
	}()
	buf := make([]byte, 2048)
	for {
		n, _, err := pc.ReadFrom(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if _, err := w.Write(buf[:n]); err != nil {
			return err
		}
	}
}
