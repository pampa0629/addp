package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/addp/common/dataprofile"
	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type DataProfileRepository struct {
	db *gorm.DB
}

func NewDataProfileRepository(db *gorm.DB) *DataProfileRepository {
	return &DataProfileRepository{db: db}
}

func (r *DataProfileRepository) GetCurrent(
	ctx context.Context,
	tenantID uint,
	itemFingerprint string,
	mode string,
	configHash string,
) (*models.DataProfile, *dataprofile.Profile, error) {
	var state models.DataProfile
	err := r.db.WithContext(ctx).
		Where(
			"tenant_id = ? AND item_fingerprint = ? AND profile_mode = ? AND profile_config_hash = ?",
			tenantID,
			itemFingerprint,
			mode,
			configHash,
		).
		Preload("Fields", func(db *gorm.DB) *gorm.DB {
			return db.Order("position ASC")
		}).
		First(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}

	profile, err := decodeStoredProfile(&state)
	if err != nil {
		return nil, nil, err
	}
	return &state, profile, nil
}

func (r *DataProfileRepository) ReplaceCurrent(
	ctx context.Context,
	state *models.DataProfile,
	profile dataprofile.Profile,
) error {
	if state == nil {
		return errors.New("data profile state is required")
	}
	observations, err := json.Marshal(profile.Observations)
	if err != nil {
		return fmt.Errorf("marshal profile observations: %w", err)
	}
	state.Observations = observations
	dataScope, err := json.Marshal(profile.DataScope)
	if err != nil {
		return fmt.Errorf("marshal profile data scope: %w", err)
	}
	state.DataScope = dataScope
	state.SchemaVersion = profile.SchemaVersion
	state.ProfileMode = profile.Mode
	state.SampleMethod = profile.SampleMethod
	state.SampleSize = profile.SampleSize
	state.RowsScanned = profile.RowsScanned
	state.RowCount = profile.RowCount
	state.RowCountExact = profile.RowCountExact
	state.FieldCount = profile.FieldCount
	state.Truncated = profile.Truncated
	state.Partial = profile.Partial
	state.ProfiledAt = profile.ProfiledAt

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current models.DataProfile
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(
				"tenant_id = ? AND item_fingerprint = ? AND profile_mode = ? AND profile_config_hash = ?",
				state.TenantID,
				state.ItemFingerprint,
				state.ProfileMode,
				state.ProfileConfigHash,
			).
			First(&current).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			if err := tx.Omit("Fields").Create(state).Error; err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			state.ID = current.ID
			state.CreatedAt = current.CreatedAt
			if err := tx.Omit("Fields").Save(state).Error; err != nil {
				return err
			}
		}

		if err := tx.Where("profile_id = ?", state.ID).Delete(&models.DataProfileField{}).Error; err != nil {
			return err
		}
		fields := make([]models.DataProfileField, 0, len(profile.Fields))
		for position, field := range profile.Fields {
			payload, err := json.Marshal(field)
			if err != nil {
				return fmt.Errorf("marshal profile field %q: %w", field.Name, err)
			}
			fields = append(fields, models.DataProfileField{
				ProfileID: state.ID,
				Position:  position,
				Name:      field.Name,
				Type:      string(field.Type),
				Status:    field.Status,
				Profile:   payload,
			})
		}
		if len(fields) == 0 {
			return nil
		}
		return tx.Create(&fields).Error
	})
}

func decodeStoredProfile(state *models.DataProfile) (*dataprofile.Profile, error) {
	profile := &dataprofile.Profile{
		SchemaVersion: state.SchemaVersion,
		Mode:          state.ProfileMode,
		SampleMethod:  state.SampleMethod,
		SampleSize:    state.SampleSize,
		RowsScanned:   state.RowsScanned,
		RowCount:      state.RowCount,
		RowCountExact: state.RowCountExact,
		FieldCount:    state.FieldCount,
		Truncated:     state.Truncated,
		Partial:       state.Partial,
		ProfiledAt:    state.ProfiledAt,
		Fields:        make([]dataprofile.FieldProfile, 0, len(state.Fields)),
	}
	if err := json.Unmarshal(state.DataScope, &profile.DataScope); err != nil {
		return nil, fmt.Errorf("decode profile data scope: %w", err)
	}
	if len(state.Observations) > 0 {
		if err := json.Unmarshal(state.Observations, &profile.Observations); err != nil {
			return nil, fmt.Errorf("decode profile observations: %w", err)
		}
	}
	for _, stored := range state.Fields {
		var field dataprofile.FieldProfile
		if err := json.Unmarshal(stored.Profile, &field); err != nil {
			return nil, fmt.Errorf("decode profile field %q: %w", stored.Name, err)
		}
		profile.Fields = append(profile.Fields, field)
	}
	return profile, nil
}
