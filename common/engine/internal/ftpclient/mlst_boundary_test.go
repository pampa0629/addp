package ftpclient

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// A strict file engine must reject unknown or missing MLST types.
func TestStrictMLSTFacts(t *testing.T) {
	for _, fact := range []string{"type=OS.unix=slink;size=5;modify=20261020010101;", "size=5;modify=20261020010101;"} {
		t.Run(fact, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				fmt.Fprint(conn, "220 test\r\n")
				scanner := bufio.NewScanner(conn)
				for scanner.Scan() {
					line := scanner.Text()
					command := strings.SplitN(line, " ", 2)[0]
					switch command {
					case "USER":
						fmt.Fprint(conn, "331 password\r\n")
					case "PASS":
						fmt.Fprint(conn, "230 logged in\r\n")
					case "FEAT":
						fmt.Fprint(conn, "211-Features\r\n MLST type*;size*;modify*;\r\n REST STREAM\r\n211 End\r\n")
					case "TYPE":
						fmt.Fprint(conn, "200 binary\r\n")
					case "MLST":
						fmt.Fprintf(conn, "250-Listing\r\n %s /link\r\n250 End\r\n", fact)
					case "QUIT":
						return
					default:
						fmt.Fprint(conn, "500 unsupported\r\n")
					}
				}
			}()
			c := Config{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, User: "reader", Password: "secret", Security: "plain", Timeout: time.Second}
			s, err := Dial(t.Context(), c)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			entry, err := s.Stat("/link")
			if err == nil {
				t.Fatalf("unsupported or missing MLST type accepted as %v", entry.Type)
			}
		})
	}
}
