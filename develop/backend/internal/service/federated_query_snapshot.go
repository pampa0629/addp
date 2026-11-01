package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	commonExecution "github.com/addp/common/execution"
	commoni18n "github.com/addp/common/middleware/i18n"
	developi18n "github.com/addp/develop/backend/i18n"
	"github.com/addp/develop/backend/internal/models"
)

const federatedBindingSchema = "addp.federated_query_binding/v1"

var errFederatedBindingInvalid = errors.New("invalid federated query execution binding")

func federatedBindingError(ctx context.Context) error {
	return fmt.Errorf("%s", commoni18n.ForLanguage(commoni18n.Language(ctx), developi18n.MsgFederatedBindingInvalid))
}

func validateFederatedBinding(binding *models.FederatedQueryBinding, selectedID uint) error {
	if binding == nil || binding.SchemaVersion != federatedBindingSchema || selectedID == 0 ||
		binding.SelectedEngineID != selectedID || binding.RuntimeEngineID == 0 ||
		binding.Audience == "" || strings.TrimSpace(binding.Audience) != binding.Audience || len(binding.SourceEngineIDs) == 0 {
		return errFederatedBindingInvalid
	}
	seen := map[uint]bool{}
	for _, id := range binding.SourceEngineIDs {
		if id == 0 || id == selectedID || id == binding.RuntimeEngineID || seen[id] {
			return errFederatedBindingInvalid
		}
		seen[id] = true
	}
	return nil
}

func cloneFederatedBinding(binding *models.FederatedQueryBinding) *models.FederatedQueryBinding {
	if binding == nil {
		return nil
	}
	copy := *binding
	copy.SourceEngineIDs = append([]uint(nil), binding.SourceEngineIDs...)
	return &copy
}

func decodeFederatedBinding(value interface{}, selectedID uint) (*models.FederatedQueryBinding, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, errFederatedBindingInvalid
	}
	var binding models.FederatedQueryBinding
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&binding) != nil {
		return nil, errFederatedBindingInvalid
	}
	if err := validateFederatedBinding(&binding, selectedID); err != nil {
		return nil, err
	}
	return &binding, nil
}

func (e *DevExecutor) freezeFederatedExecution(ctx context.Context, task *models.DevTask, tenantID uint) error {
	if !e.isFederatedQuery(ctx, task, tenantID) {
		return nil
	}
	if task.GetEngineID() == nil {
		return federatedBindingError(ctx)
	}
	if task.FederatedBinding != nil {
		if err := validateFederatedBinding(task.FederatedBinding, *task.GetEngineID()); err != nil {
			return federatedBindingError(ctx)
		}
		return nil
	}
	query, _ := task.Content["query"].(string)
	effect, err := ClassifySQLExecutionEffect(query)
	if err != nil || effect != SQLExecutionEffectRead {
		return ErrSQLExecutionUnclassifiable
	}
	binding, err := e.federatedQuery.FreezeBinding(ctx, tenantID, *task.GetEngineID(), query)
	if err != nil {
		return err
	}
	if err := validateFederatedBinding(binding, *task.GetEngineID()); err != nil {
		return federatedBindingError(ctx)
	}
	task.FederatedBinding = cloneFederatedBinding(binding)
	return nil
}

func federatedAuthorizationEngineIDs(binding *models.FederatedQueryBinding) ([]uint, error) {
	if binding == nil {
		return nil, errFederatedBindingInvalid
	}
	if err := validateFederatedBinding(binding, binding.SelectedEngineID); err != nil {
		return nil, err
	}
	ids := append([]uint(nil), binding.SourceEngineIDs...)
	if binding.SelectedEngineID != binding.RuntimeEngineID {
		ids = append(ids, binding.SelectedEngineID)
	}
	return ids, nil
}

func restoreFederatedExecution(execution *commonExecution.TaskExecution, task *models.DevTask) error {
	value, exists := execution.ExecutionConfig["federated_query_binding"]
	if !exists {
		return nil
	}
	binding, err := decodeFederatedBinding(value, *task.GetEngineID())
	if err != nil {
		return err
	}
	task.FederatedBinding = binding
	return nil
}
