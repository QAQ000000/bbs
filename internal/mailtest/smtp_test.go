package mailtest

import (
	"bufio"
	"fmt"
	"net"
	"net/textproto"
	"strconv"
	"testing"
	"time"
)

// Closing a fixture must join even when DATA is blocked on an undrained capture
// channel, so a failed test or killed recovery worker cannot hang cleanup.
func TestCloseWithBlockedCapture(t *testing.T) {
	s := New(t)
	for i := 0; i < cap(s.Messages); i++ {
		s.Messages <- "occupied"
	}
	c, err := net.DialTimeout("tcp", net.JoinHostPort(s.Host, strconv.Itoa(s.Port)), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	r := textproto.NewReader(bufio.NewReader(c))
	if _, _, err = r.ReadResponse(220); err != nil {
		t.Fatal(err)
	}
	if _, err = fmt.Fprint(c, "DATA\r\n"); err != nil {
		t.Fatal(err)
	}
	if _, _, err = r.ReadResponse(354); err != nil {
		t.Fatal(err)
	}
	if _, err = fmt.Fprint(c, "Subject: fixture\r\n\r\nbody\r\n.\r\n"); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { s.Close(); s.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Close blocked on DATA capture")
	}
}
