package sender

import (
	"net"
	"net/textproto"
	"strings"
	"testing"
	"time"
)

// fakeServer speaks just enough SMTP to inspect what the client does.
// It advertises the extensions in advertise and answers EHLO with them.
func fakeServer(t *testing.T, advertise []string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				textConn := textproto.NewConn(conn)
				if err := textConn.PrintfLine("220 fake ESMTP"); err != nil {
					return
				}
				for {
					line, err := textConn.ReadLine()
					if err != nil {
						return
					}
					switch cmd := strings.ToUpper(line); {
					case strings.HasPrefix(cmd, "EHLO"):
						if err := textConn.PrintfLine("250-fake"); err != nil {
							return
						}
						if err := textConn.PrintfLine("250 %s", strings.Join(advertise, " ")); err != nil {
							return
						}
					case strings.HasPrefix(cmd, "QUIT"):
						textConn.PrintfLine("%s", "221 bye")
						return
					default:
						if err := textConn.PrintfLine("502 not implemented"); err != nil {
							return
						}
					}
				}
			}()
		}
	}()
	return listener.Addr().String()
}

func TestSendRequiresSTARTTLS(t *testing.T) {
	cfg := Config{Addr: fakeServer(t, []string{"AUTH PLAIN"}), RequireTLS: true, Timeout: 5 * time.Second}
	err := cfg.Send("a@example.com", []string{"b@example.com"}, []byte("Subject: x\r\n\r\nhi\r\n"))
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("got %v, want a STARTTLS error", err)
	}
}

func TestSendRequiresAUTH(t *testing.T) {
	// TLS is disabled here, so the AUTH support check is what fails.
	cfg := Config{Addr: fakeServer(t, []string{"SIZE 1024"}), Username: "u", Password: "p", Timeout: 5 * time.Second}
	err := cfg.Send("a@example.com", []string{"b@example.com"}, []byte("Subject: x\r\n\r\nhi\r\n"))
	if err == nil || !strings.Contains(err.Error(), "AUTH") {
		t.Fatalf("got %v, want an AUTH error", err)
	}
}

func TestSendTimesOut(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		time.Sleep(2 * time.Second) // never send the greeting
	}()

	cfg := Config{Addr: listener.Addr().String(), RequireTLS: true, Timeout: 100 * time.Millisecond}
	start := time.Now()
	err = cfg.Send("a@example.com", []string{"b@example.com"}, nil)
	if err == nil {
		t.Fatal("want timeout error, got nil")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("took %v, want the 100ms deadline to apply", elapsed)
	}
}

func TestSendBadAddr(t *testing.T) {
	cfg := Config{Addr: "no-port"}
	if err := cfg.Send("a@example.com", nil, nil); err == nil {
		t.Fatal("want error for address without port")
	}
}
