package contentio

import "time"

type Stat struct {
	Ref         Ref        `json:"ref"`
	Size        int64      `json:"size,omitempty"`
	ContentType string     `json:"content_type,omitempty"`
	ModifiedAt  *time.Time `json:"modified_at,omitempty"`
	Exists      bool       `json:"exists"`
	Children    int64      `json:"children,omitempty"`
}
