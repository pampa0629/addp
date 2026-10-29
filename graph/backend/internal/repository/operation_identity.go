package repository

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"

	commonapi "github.com/addp/common/api"
	"github.com/addp/graph/internal/models"
)

func validDigest(value string) bool {
	b, err := hex.DecodeString(value)
	return err == nil && len(b) == sha256.Size && hex.EncodeToString(b) == value
}
func tokenDigest(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}
func validateOperation(op models.GraphOperation) error {
	hash := sha256.Sum256([]byte(op.TargetDescriptor))
	if op.ID == "" || op.TenantID == 0 || op.GraphID == 0 || !validDigest(op.RequestDigest) || !json.Valid([]byte(op.TargetDescriptor)) || op.TargetDigest != hex.EncodeToString(hash[:]) {
		return fmt.Errorf("%w: incomplete graph operation identity", commonapi.ErrConflict)
	}
	var target models.GraphOperationTarget
	decoder := json.NewDecoder(bytes.NewReader([]byte(op.TargetDescriptor)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&target); err != nil {
		return commonapi.ErrConflict
	}
	if target.EngineID == 0 || target.EngineVersion <= 0 || target.CatalogPath.EngineID != target.EngineID || len(target.CatalogPath.Segments) == 0 || (target.IndexManagement != models.GraphIndexesManaged && target.IndexManagement != models.GraphIndexesExternal) {
		return commonapi.ErrConflict
	}
	switch op.Kind {
	case "copilot", "graph", "graph_indexes", "graph_constraint":
	default:
		return commonapi.ErrConflict
	}
	return nil
}

func operationMatchesGraph(op models.GraphOperation, kg models.KnowledgeGraph, engineVersion int64) bool {
	var target models.GraphOperationTarget
	if json.Unmarshal([]byte(op.TargetDescriptor), &target) != nil {
		return false
	}
	return target.EngineID == kg.EngineID && target.IndexManagement == kg.IndexManagement && reflect.DeepEqual(target.CatalogPath, kg.CatalogPath) && (engineVersion == 0 || target.EngineVersion == engineVersion)
}
