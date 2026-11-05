package clickhouse

import "strings"

const clickhouseBinaryColumnMarker = "addp:type=bytes"

var clickhouseCommentEscaper = strings.NewReplacer(`\`, `\\`, "'", `\'`, "\x00", `\0`)

// The declaration occupies a whole line. Human comments stay separate from
// the source-owned type fact, including when copied to another engine.
func clickhouseBinaryColumnDeclaration(nativeType, comment string) (bool, string) {
	lines := strings.Split(comment, "\n")
	kept := lines[:0]
	found := false
	for _, line := range lines {
		if strings.TrimSuffix(line, "\r") == clickhouseBinaryColumnMarker {
			found = true
			continue
		}
		kept = append(kept, line)
	}
	return found && clickhouseBinaryColumnStorage(nativeType), strings.Join(kept, "\n")
}

func clickhouseBinaryColumnStorage(nativeType string) bool {
	return nativeType == "String" || nativeType == "Nullable(String)"
}
