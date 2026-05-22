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
