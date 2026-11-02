package identitytransport

import (
	"bufio"
	"codearts2api/internal/access"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMailActualSTARTTLSConversationAndNoDowngrade(t *testing.T) {
	certServer := httptest.NewTLSServer(nil)
	certificate := certServer.TLS.Certificates[0]
	parsed, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	certServer.Close()
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	for _, supportTLS := range []bool{true, false} {
		t.Run(strconv.FormatBool(supportTLS), func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			messages := make(chan string, 1)
			done := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					done <- err
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				reader := bufio.NewReader(conn)
				fmt.Fprint(conn, "220 test SMTP\r\n")
				var body strings.Builder
				inData := false
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						if !supportTLS {
							done <- nil
						} else {
							done <- err
						}
						return
					}
					line = strings.TrimRight(line, "\r\n")
					if inData {
						if line == "." {
							inData = false
							messages <- body.String()
							fmt.Fprint(conn, "250 accepted\r\n")
						} else {
							body.WriteString(line + "\n")
						}
						continue
					}
					switch {
					case strings.HasPrefix(line, "EHLO"):
						if supportTLS {
							fmt.Fprint(conn, "250-test\r\n250-STARTTLS\r\n250 AUTH PLAIN\r\n")
						} else {
							fmt.Fprint(conn, "250 test\r\n")
						}
					case line == "STARTTLS":
						fmt.Fprint(conn, "220 begin TLS\r\n")
						secure := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
						if err := secure.Handshake(); err != nil {
							done <- err
							return
						}
						conn = secure
						reader = bufio.NewReader(conn)
					case strings.HasPrefix(line, "AUTH PLAIN"):
						fmt.Fprint(conn, "235 authenticated\r\n")
					case strings.HasPrefix(line, "MAIL FROM:") || strings.HasPrefix(line, "RCPT TO:"):
						fmt.Fprint(conn, "250 ok\r\n")
					case line == "DATA":
						inData = true
						fmt.Fprint(conn, "354 send data\r\n")
					case line == "QUIT":
						fmt.Fprint(conn, "221 bye\r\n")
						done <- nil
						return
					default:
						done <- fmt.Errorf("unexpected SMTP command %q", line)
						return
					}
				}
			}()
			port := listener.Addr().(*net.TCPAddr).Port
			client := &Client{tlsConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}
			err = client.Send(context.Background(), access.IdentitySettings{SMTPHost: "127.0.0.1", SMTPPort: port, SMTPFrom: "noreply@example.com", SMTPUser: "user", SMTPPassword: "private"}, "alice@example.com", "012345")
			if supportTLS {
				if err != nil {
					t.Fatal(err)
				}
				select {
				case message := <-messages:
					if !strings.Contains(message, "012345") || !strings.Contains(message, "To: alice@example.com") {
						t.Fatal("wrong mail", message)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("mail not delivered")
				}
			} else {
				if err == nil {
					t.Fatal("plaintext SMTP downgrade accepted")
				}
				select {
				case <-messages:
					t.Fatal("mail sent without TLS")
				default:
				}
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}
