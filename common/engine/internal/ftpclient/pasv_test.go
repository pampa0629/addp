package ftpclient

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIntegrationPASVUsesControlPeer(t *testing.T) {
	if os.Getenv("ADDP_FTP_INTEGRATION") != "1" {
		t.Skip("requires make test-common-ftp")
	}
	b, err := os.ReadFile(filepath.Join(os.Getenv("ADDP_FTP_ADMISSION_DIR"), "connection.json"))
	if err != nil {
		t.Fatal("private connection missing")
	}
	var c struct {
		Host, User, Password string
		Port                 int
	}
	if json.Unmarshal(b, &c) != nil {
		t.Fatal("invalid connection")
	}
	config := Config{Host: c.Host, Port: c.Port, User: c.User, Password: c.Password, Security: "plain", Timeout: 10 * time.Second}
	s, err := Dial(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.Conn.forcePASV = true
	conn, err := s.Conn.data("RETR", "/examples/demo.json", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.Copy(io.Discard, conn); err != nil {
		t.Fatal(err)
	}
	if err = s.Conn.complete(conn); err != nil {
		t.Fatal(err)
	}
}
