package deadletter

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// IdentityForSourceRecord 使用 task apply identity 作为 UUID v5 namespace，
// 对 source identity、partition 和原 record offset 做无分隔歧义的稳定编码。
func IdentityForSourceRecord(applyIdentity, sourceIdentity, partition string, recordOffset int64) (uuid.UUID, error) {
	applyID, err := uuid.Parse(strings.TrimSpace(applyIdentity))
	if err != nil {
		return uuid.Nil, fmt.Errorf("dead-letter apply identity must be a valid UUID: %w", err)
	}
	if strings.TrimSpace(sourceIdentity) == "" {
		return uuid.Nil, fmt.Errorf("dead-letter source identity is required")
	}
	if strings.TrimSpace(partition) == "" {
		return uuid.Nil, fmt.Errorf("dead-letter source partition is required")
	}
	if recordOffset < 0 {
		return uuid.Nil, fmt.Errorf("dead-letter source offset must be non-negative")
	}

	var name bytes.Buffer
	writeIdentityPart(&name, sourceIdentity)
	writeIdentityPart(&name, partition)
	_ = binary.Write(&name, binary.BigEndian, recordOffset)
	return uuid.NewSHA1(applyID, name.Bytes()), nil
}

func writeIdentityPart(buffer *bytes.Buffer, value string) {
	_ = binary.Write(buffer, binary.BigEndian, uint64(len(value)))
	_, _ = buffer.WriteString(value)
}
