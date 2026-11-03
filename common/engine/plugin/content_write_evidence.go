package plugin

import (
	"errors"
	"io"
	"reflect"
)

// ContentWriteEvidence describes one actual Provider write, never a Grant or a
// promise that an external process will keep the object alive afterwards.
type ContentWriteEvidence struct {
	Target      EngineCatalogPath `json:"target"`
	Disposition string            `json:"disposition" enums:"created,replaced"`
	SizeBytes   int64             `json:"size_bytes,string" swaggertype:"string"`
	Committed   bool              `json:"committed"`
}

// ContentWriteCommitter is an optional result capability of CreateContent.
// Evidence is available only after the same writer successfully closes.
type ContentWriteCommitter interface {
	io.WriteCloser
	ContentWriteEvidence() (ContentWriteEvidence, error)
}

func (e ContentWriteEvidence) Validate(target EngineCatalogPath) error {
	if _, err := RequireFileLeafPath(e.Target); err != nil {
		return err
	}
	if !reflect.DeepEqual(e.Target, target) || !e.Committed || e.SizeBytes < 0 || (e.Disposition != "created" && e.Disposition != "replaced") {
		return errors.New("invalid committed content write evidence")
	}
	return nil
}
