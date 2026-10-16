package api

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/engine/plugin"
	commoni18n "github.com/addp/common/middleware/i18n"
	manageri18n "github.com/addp/manager/i18n"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/preview"
	managerprotection "github.com/addp/manager/internal/protection"
	"github.com/gin-gonic/gin"
)

func trajectorySelectionFromQuery(c *gin.Context) (*models.TrajectorySelection, error) {
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		return nil, preview.ErrTrajectorySelection
	}
	values, present := query["trajectory_selection"]
	if !present {
		return nil, nil
	}
	for key, values := range query {
		if (key != "locator" && key != "page" && key != "page_size" && key != "trajectory_selection") || len(values) != 1 {
			return nil, preview.ErrTrajectorySelection
		}
	}
	if len(values) != 1 || query.Get("page") != "1" || query.Get("page_size") != "2000" {
		return nil, preview.ErrTrajectorySelection
	}
	return preview.ParseTrajectorySelection(values[0])
}

// Use the same effective-decision mapping as output protection. In particular,
// an active temporary raw-value grant is honoured only for its exact subject.
func checkTrajectoryProtection(selection *models.TrajectorySelection, rules []dataprotection.Rule, subject dataprotection.SubjectReference, projections []dataprotection.Projection) error {
	now := time.Now().UTC()
	for _, projection := range projections {
		if projection.Validate(now) != nil {
			return managerprotection.ErrRequired
		}
	}
	for _, field := range dataprotection.QueryOutputDerivedFields(plugin.QueryOutputSource{IdentityOutput: true}, managerprotection.ActionPreview, rules, subject, now) {
		if field == selection.DeviceField || field == selection.TimeField {
			return managerprotection.ErrRequired
		}
	}
	return nil
}

func writeTrajectoryError(c *gin.Context, err error) bool {
	status, key, code := http.StatusUnprocessableEntity, "", ""
	switch {
	case errors.Is(err, preview.ErrTrajectorySelection):
		status, key, code = http.StatusBadRequest, manageri18n.MsgTrajectorySelectionInvalid, "trajectory_selection_invalid"
	case errors.Is(err, preview.ErrTrajectoryPoints):
		key, code = manageri18n.MsgTrajectoryPointLimit, "trajectory_point_limit_exceeded"
	case errors.Is(err, preview.ErrTrajectoryBudget):
		key, code = manageri18n.MsgTrajectoryScanBudget, "trajectory_scan_budget_exceeded"
	case errors.Is(err, preview.ErrTrajectorySource):
		key, code = manageri18n.MsgTrajectorySourceInvalid, "trajectory_source_invalid"
	default:
		return false
	}
	c.JSON(status, gin.H{"error": commoni18n.T(c, key), "error_code": code})
	return true
}

// Selection counts depend on the predicate's raw-value grant. Recheck it on
// both sides of the existing final output protector, including an empty result.
func bindTrajectoryOutputProtection(result *preview.PreviewResult, check func() error, rules []dataprotection.Rule, subject dataprotection.SubjectReference) error {
	if result == nil || check == nil {
		return managerprotection.ErrRequired
	}
	table, ok := result.Data.(*models.TablePreview)
	if !ok || table == nil || table.TrajectoryScope == nil || table.PreparedProtection != nil {
		return managerprotection.ErrRequired
	}
	table.PreparedProtection = &dataprotection.PreparedTableProtection{Apply: func(output *plugin.QueryResult) error {
		if err := check(); err != nil {
			return err
		}
		if err := dataprotection.ProtectQueryResultSource(output, plugin.QueryOutputSource{IdentityOutput: true}, managerprotection.ActionPreview, rules, subject); err != nil {
			return err
		}
		return check()
	}}
	return nil
}
