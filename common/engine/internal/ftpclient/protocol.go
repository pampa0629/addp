package ftpclient

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"path"
	"strconv"
	"strings"
	"time"
)

type protocolError struct{ Code int }

func (e *protocolError) Error() string { return fmt.Sprintf("FTP server returned status %d", e.Code) }

// wire owns only the admitted read-only protocol surface. Every command uses a
// single session; there is no shared pool, retry, active mode or LIST fallback.
type wire struct {
	session   *Session
	text      *textproto.Conn
	peer      string
	tls       *tls.Config
	forcePASV bool
}

func (w *wire) command(command, arg string) (int, string, error) {
	if !validText(arg) {
		return 0, "", fmt.Errorf("invalid FTP command argument")
	}
	line := command
	if arg != "" {
		line += " " + arg
	}
	if err := w.text.PrintfLine("%s", line); err != nil {
		return 0, "", err
	}
	code, message, err := w.text.ReadResponse(0)
	if err != nil {
		return 0, "", err
	}
	return code, message, nil
}
func (w *wire) expect(command, arg string, expected int) (string, error) {
	code, message, err := w.command(command, arg)
	if err != nil {
		return "", err
	}
	if code != expected {
		return "", &protocolError{code}
	}
	return message, nil
}
func (w *wire) login(config Config) error {
	conn, err := w.session.dial(net.JoinHostPort(config.Host, strconv.Itoa(config.Port)))
	if err != nil {
		return err
	}
	w.peer = conn.RemoteAddr().(*net.TCPAddr).IP.String()
	// Bound control input too: multiline replies cannot consume unlimited memory.
	w.text = textproto.NewConn(&budgetConn{Conn: conn, left: metadataBudget})
	if _, _, err = w.text.ReadResponse(220); err != nil {
		return err
	}
	if w.tls != nil {
		if _, err = w.expect("AUTH", "TLS", 234); err != nil {
			return err
		}
		secure := tls.Client(conn, w.tls)
		if err = secure.HandshakeContext(w.session.ctx); err != nil {
			return err
		}
		w.text = textproto.NewConn(&budgetConn{Conn: secure, left: metadataBudget})
	}
	code, _, err := w.command("USER", config.User)
	if err != nil {
		return err
	}
	if code == 331 {
		if _, err = w.expect("PASS", config.Password, 230); err != nil {
			return err
		}
	} else if code != 230 {
		return &protocolError{code}
	}
	features, err := w.expect("FEAT", "", 211)
	if err != nil {
		return err
	}
	mlst, rest, utf8 := false, false, false
	for _, line := range strings.Split(features, "\n") {
		feature := strings.ToUpper(strings.TrimSpace(line))
		fields := strings.Fields(feature)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "MLST" {
			mlst = true
		}
		if feature == "REST STREAM" {
			rest = true
		}
		if feature == "UTF8" {
			utf8 = true
		}
	}
	if !mlst || !rest {
		return fmt.Errorf("FTP requires native MLST and REST STREAM")
	}
	if _, err = w.expect("TYPE", "I", 200); err != nil {
		return err
	}
	if utf8 {
		if _, err = w.expect("OPTS", "UTF8 ON", 200); err != nil {
			return err
		}
	}
	if w.tls != nil {
		if _, err = w.expect("PBSZ", "0", 200); err != nil {
			return err
		}
		if _, err = w.expect("PROT", "P", 200); err != nil {
			return err
		}
	}
	return nil
}
func passivePort(message string, epsv bool) (int, error) {
	start := strings.LastIndexByte(message, '(')
	end := strings.LastIndexByte(message, ')')
	if start < 0 || end <= start {
		return 0, fmt.Errorf("invalid FTP passive response")
	}
	value := message[start+1 : end]
	port := 0
	if epsv {
		if len(value) < 5 {
			return 0, fmt.Errorf("invalid EPSV response")
		}
		delimiter := value[:1]
		fields := strings.Split(value, delimiter)
		if len(fields) != 5 || fields[0] != "" || fields[1] != "" || fields[2] != "" || fields[4] != "" {
			return 0, fmt.Errorf("invalid EPSV tuple")
		}
		parsed, err := strconv.Atoi(fields[3])
		if err != nil {
			return 0, fmt.Errorf("invalid EPSV port")
		}
		port = parsed
	} else {
		fields := strings.Split(value, ",")
		if len(fields) != 6 {
			return 0, fmt.Errorf("invalid PASV tuple")
		}
		nums := make([]int, 6)
		for i, s := range fields {
			v, err := strconv.Atoi(s)
			if err != nil || v < 0 || v > 255 {
				return 0, fmt.Errorf("invalid PASV number")
			}
			nums[i] = v
		}
		// RFC1918/NAT advertised addresses never override the control peer.
		port = nums[4]*256 + nums[5]
	}
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("invalid FTP data port")
	}
	return port, nil
}
func (w *wire) data(command, arg string, offset int64) (net.Conn, error) {
	code, msg, err := 0, "", error(nil)
	if !w.forcePASV {
		code, msg, err = w.command("EPSV", "")
		if err != nil {
			return nil, err
		}
	}
	epsv := !w.forcePASV && code == 229
	if !epsv {
		if !w.forcePASV && code != 500 && code != 502 && code != 504 && code != 522 {
			return nil, &protocolError{code}
		}
		msg, err = w.expect("PASV", "", 227)
		if err != nil {
			return nil, err
		}
	}
	port, err := passivePort(msg, epsv)
	if err != nil {
		return nil, err
	}
	conn, err := w.session.dial(net.JoinHostPort(w.peer, strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}
	if offset > 0 {
		if _, err = w.expect("REST", strconv.FormatInt(offset, 10), 350); err != nil {
			conn.Close()
			return nil, err
		}
	}
	code, _, err = w.command(command, arg)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if code != 125 && code != 150 {
		conn.Close()
		return nil, &protocolError{code}
	}
	if w.tls != nil {
		secure := tls.Client(conn, w.tls)
		if err = secure.HandshakeContext(w.session.ctx); err != nil {
			conn.Close()
			return nil, err
		}
		return secure, nil
	}
	return conn, nil
}
func (w *wire) complete(conn net.Conn) error {
	if err := conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		return err
	}
	code, _, err := w.text.ReadResponse(0)
	if err != nil {
		return err
	}
	if code != 226 && code != 250 {
		return &protocolError{code}
	}
	return nil
}
func (s *Session) dial(address string) (net.Conn, error) {
	conn, err := (&net.Dialer{}).DialContext(s.ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	if deadline, ok := s.ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		conn.Close()
		return nil, s.ctx.Err()
	}
	s.sockets = append(s.sockets, conn)
	return conn, nil
}

type Entry struct {
	Name, Type string
	Size       int64
	Time       time.Time
}

func parseMLST(message, physical string) (*Entry, error) {
	var factsLine string
	for _, line := range strings.Split(message, "\n") {
		if !strings.HasPrefix(line, " ") {
			continue
		}
		if factsLine != "" {
			return nil, fmt.Errorf("FTP MLST must return exactly one entry")
		}
		factsLine = strings.TrimPrefix(line, " ")
	}
	factsText, name, ok := strings.Cut(factsLine, " ")
	// MLST identifies the requested object. A virtual root may be displayed as
	// the server home basename; it must never become a new catalog path.
	matchingName := name == physical || name == path.Base(physical)
	if physical == "/" {
		matchingName = name == "/" || Name(name) == nil
	}
	if !ok || !strings.HasSuffix(factsText, ";") || !validText(name) || !matchingName {
		return nil, fmt.Errorf("invalid FTP MLST resource")
	}
	facts := map[string]string{}
	for _, fact := range strings.Split(strings.TrimSuffix(factsText, ";"), ";") {
		key, value, ok := strings.Cut(fact, "=")
		key = strings.ToLower(key)
		if !ok || key == "" || value == "" {
			return nil, fmt.Errorf("invalid FTP MLST fact")
		}
		if _, exists := facts[key]; exists {
			return nil, fmt.Errorf("duplicate FTP MLST fact")
		}
		facts[key] = value
	}
	kind := strings.ToLower(facts["type"])
	if (kind != "file" && kind != "dir") || (physical == "/" && kind != "dir") {
		return nil, fmt.Errorf("unsupported or missing FTP MLST type")
	}
	modified, err := time.Parse("20060102150405.999999999", facts["modify"])
	if err != nil {
		return nil, fmt.Errorf("invalid FTP modification time")
	}
	size := int64(0)
	if kind == "file" {
		size, err = strconv.ParseInt(facts["size"], 10, 64)
		if err != nil || size < 0 {
			return nil, fmt.Errorf("invalid FTP file size")
		}
	}
	if physical == "/" {
		name = "/"
	}
	return &Entry{Name: name, Type: kind, Size: size, Time: modified.UTC()}, nil
}
func (w *wire) stat(physical string) (*Entry, error) {
	message, err := w.expect("MLST", physical, 250)
	if err != nil {
		return nil, err
	}
	return parseMLST(message, physical)
}
func (w *wire) names(physical string) ([]string, error) {
	if _, err := w.expect("CWD", physical, 250); err != nil {
		return nil, err
	}
	conn, err := w.data("NLST", "", 0)
	if err != nil {
		return nil, err
	}
	encoded, err := io.ReadAll(io.LimitReader(conn, metadataBudget+1))
	if err != nil {
		conn.Close()
		return nil, err
	}
	if len(encoded) > metadataBudget {
		conn.Close()
		return nil, fmt.Errorf("FTP directory exceeds byte budget")
	}
	if err = w.complete(conn); err != nil {
		return nil, err
	}
	if len(encoded) == 0 {
		return []string{}, nil
	}
	if !strings.HasSuffix(string(encoded), "\r\n") {
		return nil, fmt.Errorf("incomplete FTP NLST response")
	}
	names := strings.Split(strings.TrimSuffix(string(encoded), "\r\n"), "\r\n")
	if len(names) > MaxEntries {
		return nil, fmt.Errorf("FTP directory exceeds entry budget")
	}
	for _, name := range names {
		if err := Name(name); err != nil {
			return nil, err
		}
	}
	return names, nil
}
