package redis

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"strconv"
)

const maxReplyBytes = 1 << 20

// budgetConn validates one bounded RESP2 frame before exposing its headers to
// the SDK, which otherwise allocates directly from server-declared lengths.
// It wraps the already decrypted connection; TLS verification stays in the SDK.
type budgetConn struct {
	net.Conn
	reader *bufio.Reader
	frame  []byte
}

func newBudgetConn(conn net.Conn) net.Conn {
	return &budgetConn{Conn: conn, reader: bufio.NewReader(conn)}
}

func (c *budgetConn) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(c.frame) == 0 {
		var frame bytes.Buffer
		if err := readBudgetFrame(c.reader, &frame, 0); err != nil {
			_ = c.Conn.Close()
			return 0, err
		}
		c.frame = frame.Bytes()
	}
	n := copy(p, c.frame)
	c.frame = c.frame[n:]
	return n, nil
}

func readBudgetFrame(r *bufio.Reader, out *bytes.Buffer, depth int) error {
	if depth > 32 {
		return fmt.Errorf("Redis response depth exceeds budget")
	}
	line := make([]byte, 0, 64)
	for len(line) < 4096 && out.Len()+len(line) < maxReplyBytes {
		b, err := r.ReadByte()
		if err != nil {
			return err
		}
		line = append(line, b)
		if b == '\n' {
			break
		}
	}
	if len(line) < 3 || line[len(line)-2] != '\r' || line[len(line)-1] != '\n' {
		return fmt.Errorf("Redis response header exceeds budget or is invalid")
	}
	out.Write(line)
	switch line[0] {
	case '+', '-', ':':
		return nil
	case '$', '*':
		n, err := strconv.ParseInt(string(line[1:len(line)-2]), 10, 64)
		if err != nil || n < -1 {
			return fmt.Errorf("invalid Redis response length")
		}
		if n == -1 {
			return nil
		}
		if line[0] == '$' {
			if n > int64(maxReplyBytes-out.Len()-2) {
				return fmt.Errorf("Redis response bytes exceed budget")
			}
			if _, err := io.CopyN(out, r, n+2); err != nil {
				return err
			}
			b := out.Bytes()
			if b[len(b)-2] != '\r' || b[len(b)-1] != '\n' {
				return fmt.Errorf("invalid Redis bulk terminator")
			}
			return nil
		}
		if n > 16384 || n > int64((maxReplyBytes-out.Len())/3) {
			return fmt.Errorf("Redis response elements exceed budget")
		}
		for i := int64(0); i < n; i++ {
			if err := readBudgetFrame(r, out, depth+1); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported Redis response framing")
	}
}
