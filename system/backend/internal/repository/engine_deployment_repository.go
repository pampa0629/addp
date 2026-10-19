package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/models"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrDeploymentSourceOccupied = errors.New("engine deployment source is already registered")
var ErrDeploymentHostUnavailable = errors.New("engine deployment host is unavailable")

type EngineDeploymentRepository struct{ db *gorm.DB }

func NewEngineDeploymentRepository(db *gorm.DB) *EngineDeploymentRepository {
	return &EngineDeploymentRepository{db: db}
}

func (r *EngineDeploymentRepository) Transaction(ctx context.Context, fn func(*EngineDeploymentRepository) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return fn(NewEngineDeploymentRepository(tx)) })
}

func (r *EngineDeploymentRepository) RegisteredSources(ctx context.Context, keys [][3]string) (map[[3]string]string, error) {
	result := make(map[[3]string]string)
	if len(keys) == 0 {
		return result, nil
	}
	if len(keys) > 128 {
		return nil, commonapi.ErrBadRequest
	}
	query := r.db.WithContext(ctx).Model(&models.EngineDeployment{})
	for _, key := range keys {
		query = query.Or("inventory->'source'->>'docker_engine_id'=? AND inventory->'source'->>'project'=? AND inventory->'services'->0->>'service'=?", key[0], key[1], key[2])
	}
	var rows []models.EngineDeployment
	if err := query.Select("deployment_id", "inventory").Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		if len(row.Inventory.Services) == 1 {
			result[[3]string{row.Inventory.Source.DockerEngineID, row.Inventory.Source.Project, row.Inventory.Services[0].Service}] = row.DeploymentID
		}
	}
	return result, nil
}

func deploymentDBError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return commonapi.ErrNotFound
	}
	var state *pgconn.PgError
	if errors.As(err, &state) && state.Code == "23505" && state.ConstraintName == "engine_deployment_source_unique" {
		return ErrDeploymentSourceOccupied
	}
	return err
}

func projectDeployment(row *models.EngineDeployment, node models.HostNode) {
	row.NodeID, row.HostBindingState, row.HostBindingReason = "", "rejected", "node_disabled"
	if !row.Enabled {
		row.HostBindingReason = "deployment_disabled"
		return
	}
	if row.Inventory.Source.Runtime == "desktop_linux_vm" && node.NodeKind != "virtual" {
		row.HostBindingReason = "node_kind_mismatch"
		return
	}
	if node.Enabled {
		row.NodeID, row.HostBindingState, row.HostBindingReason = node.NodeID, "bound", ""
	}
}

func resolveDeploymentHosts(tx *gorm.DB, rows []models.EngineDeployment) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.DeclaredNodeID)
	}
	var nodes []models.HostNode
	if err := tx.Where("node_id IN ?", ids).Find(&nodes).Error; err != nil {
		return err
	}
	byID := make(map[string]models.HostNode, len(nodes))
	for _, node := range nodes {
		byID[node.NodeID] = node
	}
	for index := range rows {
		node, found := byID[rows[index].DeclaredNodeID]
		if !found {
			return ErrDeploymentHostUnavailable
		}
		projectDeployment(&rows[index], node)
	}
	return nil
}

func (r *EngineDeploymentRepository) Get(ctx context.Context, id string) (*models.EngineDeployment, error) {
	rows := make([]models.EngineDeployment, 1)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("deployment_id = ?", id).First(&rows[0]).Error; err != nil {
			return deploymentDBError(err)
		}
		return resolveDeploymentHosts(tx, rows)
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return &rows[0], err
}

func (r *EngineDeploymentRepository) List(ctx context.Context, page, size int, filter models.EngineDeploymentFilter) ([]models.EngineDeployment, int64, error) {
	rows := make([]models.EngineDeployment, 0)
	var total int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Model(&models.EngineDeployment{})
		if filter.Search != "" {
			pattern := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(filter.Search)) + "%"
			query = query.Where("LOWER(display_name) LIKE ? ESCAPE '!'", pattern)
		}
		if filter.NodeID != "" {
			query = query.Where("declared_node_id = ?", filter.NodeID)
		}
		if err := query.Count(&total).Error; err != nil {
			return err
		}
		if err := query.Order("created_at DESC, deployment_id ASC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
			return err
		}
		return resolveDeploymentHosts(tx, rows)
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return rows, total, err
}

// Save commits the declaration, its interval and audit together. No Docker or Tenant API is called.
func (r *EngineDeploymentRepository) Save(ctx context.Context, value *models.EngineDeployment, expected int64, audit func(*gorm.DB, *models.EngineDeployment, models.EngineDeployment) error) error {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var before *models.EngineDeployment
		if expected > 0 {
			var old models.EngineDeployment
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("deployment_id = ?", value.DeploymentID).First(&old).Error; err != nil {
				return deploymentDBError(err)
			}
			if old.Version != expected {
				return commonapi.ErrConflict
			}
			before = &old
		}
		var node models.HostNode
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("node_id = ?", value.DeclaredNodeID).First(&node).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrDeploymentHostUnavailable
			}
			return err
		}
		// A disabled existing declaration can still be retired. New/changed bindings require an enabled host.
		if !node.Enabled && (before == nil || value.Enabled || before.DeclaredNodeID != value.DeclaredNodeID || before.Inventory.Source != value.Inventory.Source) {
			return ErrDeploymentHostUnavailable
		}
		if value.Inventory.Source.Runtime == "desktop_linux_vm" && node.NodeKind != "virtual" {
			return commonapi.ErrBadRequest
		}
		var now time.Time
		if err := tx.Raw("SELECT statement_timestamp()").Scan(&now).Error; err != nil {
			return err
		}
		value.Version, value.UpdatedAt = expected+1, now.UTC()
		if before == nil {
			value.CreatedAt = now.UTC()
			if err := tx.Create(value).Error; err != nil {
				return err
			}
		} else {
			value.CreatedAt = before.CreatedAt
			result := tx.Model(&models.EngineDeployment{}).Where("deployment_id = ? AND version = ?", value.DeploymentID, expected).Select("display_name", "engine_type", "enabled", "inventory", "declared_node_id", "version", "updated_at").Updates(value)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return commonapi.ErrConflict
			}
			result = tx.Exec("UPDATE engine_deployment_declarations SET valid_to = ? WHERE deployment_id = ? AND version = ? AND valid_to IS NULL", now, value.DeploymentID, expected)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return commonapi.ErrConflict
			}
		}
		if err := tx.Exec(`INSERT INTO engine_deployment_declarations(deployment_id,version,node_id,inventory,enabled,valid_from)
			SELECT deployment_id,version,declared_node_id,inventory,enabled,updated_at FROM engine_deployments WHERE deployment_id = ?`, value.DeploymentID).Error; err != nil {
			return err
		}
		projectDeployment(value, node)
		return audit(tx, before, *value)
	})
	return deploymentDBError(err)
}
