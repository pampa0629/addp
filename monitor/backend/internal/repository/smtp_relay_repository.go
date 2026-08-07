package repository

import (
	"context"
	"errors"

	"github.com/addp/monitor/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrSMTPRelayVersionConflict = errors.New("SMTP relay version conflict")

type SMTPRelayRepository struct{ db *gorm.DB }

func NewSMTPRelayRepository(db *gorm.DB) *SMTPRelayRepository { return &SMTPRelayRepository{db: db} }

func (r *SMTPRelayRepository) Get(ctx context.Context) (*models.SMTPRelay, error) {
	var value models.SMTPRelay
	err := r.db.WithContext(ctx).First(&value, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &value, err
}

func (r *SMTPRelayRepository) Save(ctx context.Context, value *models.SMTPRelay, expected uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current models.SMTPRelay
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, 1).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if expected != 0 {
				return ErrSMTPRelayVersionConflict
			}
			value.ID, value.Version = 1, 1
			return tx.Create(value).Error
		}
		if err != nil {
			return err
		}
		if current.Version != expected {
			return ErrSMTPRelayVersionConflict
		}
		value.ID, value.Version = 1, current.Version+1
		value.CredentialCiphertext, value.CredentialVersion = current.CredentialCiphertext, current.CredentialVersion
		result := tx.Model(&models.SMTPRelay{}).Where("id = 1 AND version = ?", expected).Updates(map[string]interface{}{
			"version": value.Version, "enabled": value.Enabled, "host": value.Host, "port": value.Port,
			"tls_mode": value.TLSMode, "from_address": value.FromAddress, "from_name": value.FromName,
			"username": value.Username, "updated_by": value.UpdatedBy,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrSMTPRelayVersionConflict
		}
		return tx.First(value, 1).Error
	})
}

func (r *SMTPRelayRepository) RotateCredential(ctx context.Context, ciphertext string, updatedBy uint) (*models.SMTPRelay, error) {
	var result models.SMTPRelay
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&result, 1).Error
		notFound := errors.Is(err, gorm.ErrRecordNotFound)
		if notFound {
			result = models.SMTPRelay{ID: 1, Port: 587, TLSMode: "starttls", FromName: "ADDP Monitor"}
		}
		if err != nil && !notFound {
			return err
		}
		result.CredentialCiphertext = ciphertext
		result.CredentialVersion++
		result.UpdatedBy = updatedBy
		if notFound {
			return tx.Create(&result).Error
		}
		return tx.Model(&models.SMTPRelay{}).Where("id = 1").Updates(map[string]interface{}{
			"credential_ciphertext": ciphertext, "credential_version": result.CredentialVersion, "updated_by": updatedBy,
		}).Error
	})
	return &result, err
}
