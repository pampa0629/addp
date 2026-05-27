package datatype

// DocumentInfo is the common type info for document data items.
type DocumentInfo struct {
	Title         string `json:"title,omitempty"`
	Language      string `json:"language,omitempty"`
	Encoding      string `json:"encoding,omitempty"`
	PageCount     int    `json:"page_count,omitempty"`
	WordCount     int    `json:"word_count,omitempty"`
	SizeBytes     *int64 `json:"size_bytes,omitempty"`
	TextExtracted bool   `json:"text_extracted"`
}

// Clone returns a deep copy of DocumentInfo.
func (d *DocumentInfo) Clone() *DocumentInfo {
	if d == nil {
		return nil
	}
	cloned := *d
	if d.SizeBytes != nil {
		sizeBytes := *d.SizeBytes
		cloned.SizeBytes = &sizeBytes
	}
	return &cloned
}
