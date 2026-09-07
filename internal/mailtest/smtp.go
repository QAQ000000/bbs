// Package mailtest provides a loopback-only SMTP fixture for integration tests.
package mailtest

import (
	"bufio"
	"fmt"
	"net"
	"net/textproto"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type SMTP struct {
	Host     string
	Port     int
	Messages chan string
	Reject   atomic.Int32
	DropQuit atomic.Bool
	listener net.Listener
	mu       sync.Mutex
	conns    map[net.Conn]bool
	wg       sync.WaitGroup
}

func New(t testing.TB) *SMTP {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &SMTP{Host: "127.0.0.1", Port: l.Addr().(*net.TCPAddr).Port, Messages: make(chan string, 32), listener: l, conns: map[net.Conn]bool{}}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			s.mu.Lock()
			s.conns[c] = true
			s.mu.Unlock()
			s.wg.Add(1)
			go s.serve(c)
		}
	}()
	t.Cleanup(func() {
		_ = l.Close()
		s.mu.Lock()
		for c := range s.conns {
			_ = c.Close()
		}
		s.mu.Unlock()
		s.wg.Wait()
	})
	return s
}

func (s *SMTP) serve(c net.Conn) {
	defer s.wg.Done()
	defer c.Close()
	defer func() { s.mu.Lock(); delete(s.conns, c); s.mu.Unlock() }()
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	r := textproto.NewReader(bufio.NewReader(c))
	_, _ = fmt.Fprint(c, "220 test.local ESMTP\r\n")
	for {
		line, err := r.ReadLine()
		if err != nil {
			return
		}
		verb := strings.ToUpper(strings.Fields(line + " ")[0])
		switch verb {
		case "EHLO", "HELO":
			_, _ = fmt.Fprint(c, "250 test.local\r\n")
		case "MAIL":
			_, _ = fmt.Fprint(c, "250 OK\r\n")
		case "RCPT":
			if code := s.Reject.Load(); code != 0 {
				_, _ = fmt.Fprintf(c, "%d private SMTP diagnostic\r\n", code)
			} else {
				_, _ = fmt.Fprint(c, "250 OK\r\n")
			}
		case "DATA":
			_, _ = fmt.Fprint(c, "354 continue\r\n")
			body, err := r.ReadDotBytes()
			if err != nil {
				return
			}
			s.Messages <- string(body)
			_, _ = fmt.Fprint(c, "250 accepted\r\n")
		case "QUIT":
			if !s.DropQuit.Load() {
				_, _ = fmt.Fprint(c, "221 bye\r\n")
			}
			return
		default:
			_, _ = fmt.Fprint(c, "500 unsupported\r\n")
		}
	}
}
