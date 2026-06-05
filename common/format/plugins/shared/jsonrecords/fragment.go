package jsonrecords

import "bytes"

func JSONArrayFragment(data []byte) []byte {
	objects := jsonObjectFragments(data)
	if len(objects) == 0 {
		return []byte("[]")
	}
	total := 2
	for _, object := range objects {
		total += len(object) + 1
	}
	out := make([]byte, 0, total)
	out = append(out, '[')
	for i, object := range objects {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, object...)
	}
	out = append(out, ']')
	return out
}

func jsonObjectFragments(data []byte) [][]byte {
	fragments := make([][]byte, 0)
	depth := 0
	start := -1
	inString := false
	escaped := false
	for i, b := range data {
		if inString {
			switch {
			case escaped:
				escaped = false
			case b == '\\':
				escaped = true
			case b == '"':
				inString = false
			}
			continue
		}
		switch b {
		case '"':
			if depth > 0 {
				inString = true
			}
		case '{':
			if depth == 0 {
				start = i
			}
			depth++
		case '}':
			if depth == 0 {
				continue
			}
			depth--
			if depth == 0 && start >= 0 {
				fragments = append(fragments, bytes.TrimSpace(data[start:i+1]))
				start = -1
			}
		}
	}
	return fragments
}
