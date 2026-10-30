// Package ftpclient owns one strictly bounded FTP session per operation.
// It has no engine registration; protocol admission precedes the public plugin.
package ftpclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const MaxEntries = 10000
const metadataBudget = 4 << 20

type Config struct {
	Host, User, Password, Security, CAPEM string
	Port                                  int
	Timeout                               time.Duration
}

func validText(s string) bool { return utf8.ValidString(s) && !strings.ContainsAny(s, "\r\n\x00") }

func Name(s string) error {
	if s == "" || s == "." || s == ".." || !validText(s) || strings.ContainsAny(s, "/\\") {
		return fmt.Errorf("invalid FTP path component")
	}
	return nil
}

func Relative(s string) error {
	if s == "" {
		return nil
	}
	for _, n := range strings.Split(s, "/") {
		if err := Name(n); err != nil {
			return err
		}
	}
	return nil
}

func (c Config) Validate() error {
	if c.Host == "" || !validText(c.Host) || strings.ContainsAny(c.Host, "/\\ @") || c.Port < 1 || c.Port > 65535 || c.User == "" || !validText(c.User) || c.Password == "" || !validText(c.Password) {
		return fmt.Errorf("invalid FTP connection facts")
	}
	if c.Security != "explicit_tls" && c.Security != "plain" {
		return fmt.Errorf("FTP transport security must be explicit")
	}
	if c.Security == "plain" && c.CAPEM != "" {
		return fmt.Errorf("FTP CA requires explicit TLS")
	}
	if c.Timeout <= 0 || c.Timeout > 5*time.Minute {
		return fmt.Errorf("invalid FTP operation timeout")
	}
	return nil
}

func Physical(relative string) (string, error) {
	if err := Relative(relative); err != nil {
		return "", err
	}
	if relative == "" {
		return "/", nil
	}
	return "/" + relative, nil
}

// Session is never shared. Closing/cancelling it closes both control and data
// sockets, including sockets currently blocked in TLS or protocol negotiation.
type Session struct {
	Conn    *wire
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	sockets []net.Conn
	closed  bool
	stop    func() bool
}

func Dial(ctx context.Context, config Config) (*Session, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, config.Timeout)
	s := &Session{ctx: ctx, cancel: cancel}
	s.stop = context.AfterFunc(ctx, s.closeSockets)
	var tlsConfig *tls.Config
	if config.Security == "explicit_tls" {
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		if config.CAPEM != "" && !roots.AppendCertsFromPEM([]byte(config.CAPEM)) {
			s.Close()
			return nil, fmt.Errorf("invalid FTP CA certificate")
		}
		tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: config.Host, ClientSessionCache: tls.NewLRUClientSessionCache(8)}
	}
	s.Conn = &wire{session: s, tls: tlsConfig}
	if err := s.Conn.login(config); err != nil {
		failure := s.failure("connect", err)
		s.Close()
		return nil, failure
	}
	return s, nil
}

func (s *Session) closeSockets() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	for _, conn := range s.sockets {
		_ = conn.Close()
	}
}
func (s *Session) Close() error {
	if s.stop != nil {
		s.stop()
	}
	s.closeSockets()
	s.cancel()
	return nil
}
func (s *Session) failure(op string, err error) error {
	if s.ctx.Err() != nil {
		return s.ctx.Err()
	}
	// Do not forward server text (it can contain paths or credentials).
	return fmt.Errorf("FTP %s failed: %w", op, safeError(err))
}
func safeError(err error) error {
	if failure, ok := err.(*loginError); ok {
		return fmt.Errorf("%s: %w", failure.stage, safeError(failure.cause))
	}
	var pe *protocolError
	if errors.As(err, &pe) {
		return &protocolError{Code: pe.Code}
	}
	var response *textproto.Error
	if errors.As(err, &response) {
		return &protocolError{Code: response.Code}
	}
	// EOF during negotiation is a failed connection, never content EOF. Do not
	// unwrap it into io.EOF: consumers must not mistake failure for empty data.
	if errors.Is(err, io.EOF) {
		return errors.New("connection closed (EOF)")
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return context.DeadlineExceeded
	}
	return errors.New("remote operation rejected or invalid response")
}

type budgetConn struct {
	net.Conn
	left int64
}

func (c *budgetConn) Read(p []byte) (int, error) {
	if c.left <= 0 {
		return 0, fmt.Errorf("FTP metadata exceeds budget")
	}
	if int64(len(p)) > c.left {
		p = p[:c.left]
	}
	n, err := c.Conn.Read(p)
	c.left -= int64(n)
	return n, err
}

func (s *Session) Stat(physical string) (*Entry, error) {
	e, err := s.Conn.stat(physical)
	if err != nil {
		return nil, s.failure("MLST", err)
	}
	return e, nil
}

// Resolve verifies protocol facts for every ancestor and the final object.
// Explicitly reported links are rejected; server-dereferenced links are not
// distinguishable from ordinary objects. The server account owns isolation.
func (s *Session) Resolve(relative string) (*Entry, error) {
	physical, err := Physical(relative)
	if err != nil {
		return nil, err
	}
	if physical == "/" {
		return s.Stat("/")
	}
	current := ""
	parts := strings.Split(strings.TrimPrefix(physical, "/"), "/")
	var entry *Entry
	for i, name := range parts {
		current += "/" + name
		entry, err = s.Stat(current)
		if err != nil {
			return nil, err
		}
		if i < len(parts)-1 && entry.Type != "dir" {
			return nil, fmt.Errorf("FTP ancestor is not a directory")
		}
	}
	return entry, nil
}
func (s *Session) List(physical string) ([]*Entry, error) {
	root, err := s.Stat(physical)
	if err != nil {
		return nil, err
	}
	if root.Type != "dir" {
		return nil, fmt.Errorf("FTP listing requires directory")
	}
	names, err := s.Conn.names(physical)
	if err != nil {
		return nil, s.failure("NLST", err)
	}
	seen := map[string]bool{}
	result := make([]*Entry, 0, len(names))
	for _, name := range names {
		if seen[name] {
			return nil, fmt.Errorf("duplicate FTP directory entry")
		}
		seen[name] = true
		prefix := strings.TrimSuffix(physical, "/") + "/"
		e, err := s.Stat(prefix + name)
		if err != nil {
			return nil, err
		}
		e.Name = name
		result = append(result, e)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

type reader struct {
	s             *Session
	response      net.Conn
	remaining     int64
	bounded, done bool
}

func Open(ctx context.Context, config Config, relative string, offset, length int64) (io.ReadCloser, error) {
	if offset < 0 || length < 0 {
		return nil, fmt.Errorf("invalid FTP read window")
	}
	physical, err := Physical(relative)
	if err != nil {
		return nil, err
	}
	s, err := Dial(ctx, config)
	if err != nil {
		return nil, err
	}
	e, err := s.Resolve(relative)
	if err != nil {
		s.Close()
		return nil, err
	}
	if e.Type != "file" || offset > e.Size {
		s.Close()
		return nil, fmt.Errorf("invalid FTP file or offset")
	}
	resp, err := s.Conn.data("RETR", physical, offset)
	if err != nil {
		failure := s.failure("REST/RETR", err)
		s.Close()
		return nil, failure
	}
	expected := e.Size - offset
	bounded := length > 0 && length < expected
	if bounded {
		expected = length
	}
	return &reader{s: s, response: resp, remaining: expected, bounded: bounded}, nil
}
func (r *reader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.done {
		return 0, io.EOF
	}
	if err := r.s.ctx.Err(); err != nil {
		r.done = true
		r.s.Close()
		return 0, err
	}
	if r.remaining == 0 && r.bounded {
		r.done = true
		r.s.Close()
		return 0, io.EOF
	}
	if int64(len(p)) > r.remaining && r.remaining > 0 {
		p = p[:r.remaining]
	}
	if r.remaining == 0 && !r.bounded {
		var probe [1]byte
		n, err := r.response.Read(probe[:])
		if n > 0 {
			r.done = true
			r.s.Close()
			return 0, fmt.Errorf("FTP file size changed while reading")
		}
		if err == io.EOF {
			err = r.s.Conn.complete(r.response)
			if err == nil {
				err = io.EOF
			}
		}
		if err != nil {
			r.done = true
			if err != io.EOF {
				err = r.s.failure("read", err)
			}
			r.s.Close()
		}
		return 0, err
	}
	n, err := r.response.Read(p)
	r.remaining -= int64(n)
	if err != nil {
		if err == io.EOF {
			if r.remaining > 0 {
				r.done = true
				r.s.Close()
				return n, io.ErrUnexpectedEOF
			}
			err = r.s.Conn.complete(r.response)
			if err == nil {
				err = io.EOF
			}
		}
		r.done = true
		if err != io.EOF {
			err = r.s.failure("read", err)
		}
		r.s.Close()
	}
	return n, err
}
func (r *reader) Close() error { return r.s.Close() }
