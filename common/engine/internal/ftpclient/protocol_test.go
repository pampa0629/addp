package ftpclient

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func TestStrictMLSTParser(t *testing.T) {
	valid := "File details\n type=file;size=5;modify=20261020010101.123; /中文 空格%\nEnd"
	e, err := parseMLST(valid, "/中文 空格%")
	if err != nil || e.Size != 5 || e.Time.Nanosecond() != 123000000 {
		t.Fatal("lost exact facts", err)
	}
	for _, invalid := range []string{
		strings.Replace(valid, "type=file;", "", 1),
		strings.Replace(valid, "type=file;", "type=OS.unix=slink;", 1),
		strings.Replace(valid, "size=5;", "", 1),
		strings.Replace(valid, "size=5;", "size=-1;", 1),
		strings.Replace(valid, "size=5;", "size=18446744073709551615;", 1),
		strings.Replace(valid, "size=5;", "size=5;Size=6;", 1),
		strings.Replace(valid, "modify=20261020010101.123;", "", 1),
		strings.Replace(valid, " /中文 空格%", " /other", 1),
		valid + "\n type=file;size=0;modify=20261020010101; /中文 空格%",
	} {
		if _, err := parseMLST(invalid, "/中文 空格%"); err == nil {
			t.Fatal("accepted incomplete, conflicting or foreign facts")
		}
	}
}

func TestStrictPassiveReplies(t *testing.T) {
	for _, row := range []struct {
		text string
		epsv bool
		port int
	}{
		{"Entering extended passive (|||50001|)", true, 50001},
		{"Entering passive (203,0,113,123,195,81)", false, 50001},
		{"(|||0|)", true, 0}, {"(|||65536|)", true, 0}, {"(|host|tcp|50001|)", true, 0},
		{"(203,0,113,123,256,1)", false, 0}, {"(127,0,0,1,1)", false, 0},
	} {
		port, err := passivePort(row.text, row.epsv)
		if row.port == 0 {
			if err == nil {
				t.Fatal("invalid passive endpoint accepted")
			}
		} else if err != nil || port != row.port {
			t.Fatal("valid passive port rejected")
		}
	}
}

func TestTLSFailureNeverSendsCredentials(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	commands := make(chan []string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(time.Second))
		fmt.Fprint(conn, "220 plain only\r\n")
		scanner := bufio.NewScanner(conn)
		seen := []string{}
		for scanner.Scan() {
			seen = append(seen, scanner.Text())
			fmt.Fprint(conn, "502 TLS unavailable\r\n")
		}
		commands <- seen
	}()
	c := Config{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, User: "private-user", Password: "private-password", Root: "/", Security: "explicit_tls", Timeout: time.Second}
	if s, err := Dial(t.Context(), c); err == nil {
		s.Close()
		t.Fatal("TLS failure downgraded")
	}
	select {
	case seen := <-commands:
		if len(seen) != 1 || seen[0] != "AUTH TLS" {
			t.Fatal("credentials sent before protected session")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("failed TLS session not closed")
	}
}
