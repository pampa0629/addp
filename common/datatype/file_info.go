package datatype

// FileInfo is the common type info for low-semantics file data items.
type FileInfo struct {
	MIMEType  string `json:"mime_type,omitempty"`
	Encoding  string `json:"encoding,omitempty"`
	SizeBytes *int64 `json:"size_bytes,omitempty"`
}
