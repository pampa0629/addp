package repository

import (
	"fmt"
	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	"github.com/google/uuid"
	"path"
	"strings"
	"unicode/utf8"
)

// FileInputObject is an exact storage identity, never a deletion prefix.
type FileInputObject struct {
	Bucket string `json:"bucket"`
	Object string `json:"object"`
}

// FileInputPreparation survives failed attempts and result soft deletion.
// Cleaned means the writer is known to have stopped and every planned object
// has been removed. Watch retains an uncertain writer's exact tombstones.
type FileInputPreparation struct {
	ExecutionID   string            `json:"execution_id"`
	Attempt       int               `json:"attempt"`
	PreparationID string            `json:"preparation_id"`
	Objects       []FileInputObject `json:"objects"`
	Released      bool              `json:"released"`
	Watch         bool              `json:"watch"`
	Cleaned       bool              `json:"cleaned"`
}

func (p FileInputPreparation) Prefix(tenantID uint, taskType string) (string, error) {
	execution, err := uuid.Parse(p.ExecutionID)
	preparation, prepErr := uuid.Parse(p.PreparationID)
	if tenantID == 0 || err != nil || prepErr != nil || execution == uuid.Nil || preparation == uuid.Nil || execution.String() != p.ExecutionID || preparation.String() != p.PreparationID || p.Attempt <= 0 {
		return "", commonAPI.ErrConflict
	}
	namespace := ""
	switch taskType {
	case commonExecution.TaskTypeModel3DGLBGeneration:
		namespace = "model3d-input"
	case commonExecution.TaskTypeRasterCOGGeneration:
		namespace = "raster-input"
	default:
		return "", commonAPI.ErrConflict
	}
	return fmt.Sprintf("temp/%s/tenant_%d/%s/attempt_%d/%s/", namespace, tenantID, p.ExecutionID, p.Attempt, p.PreparationID), nil
}

func (p FileInputPreparation) ValidateForTask(tenantID uint, taskType string) error {
	prefix, err := p.Prefix(tenantID, taskType)
	if err != nil || len(p.Objects) == 0 || len(p.Objects) > 200 || (p.Cleaned && (!p.Released || p.Watch)) || (p.Watch && !p.Released) {
		return commonAPI.ErrConflict
	}
	seen := make(map[FileInputObject]bool, len(p.Objects))
	for _, object := range p.Objects {
		if object.Bucket == "" || object.Bucket != strings.TrimSpace(object.Bucket) || strings.ContainsAny(object.Bucket, "/\\\x00") || !strings.HasPrefix(object.Object, prefix) || len(object.Object) == len(prefix) || !utf8.ValidString(object.Object) || strings.ContainsAny(object.Object, "\\\x00\r\n:") || path.Clean(object.Object) != object.Object || seen[object] {
			return commonAPI.ErrConflict
		}
		seen[object] = true
	}
	return nil
}
