package models

import "time"

// SMTPRelay is Monitor's platform-scoped outbound mail resource.
type SMTPRelay struct {
	ID                   uint      `gorm:"primaryKey;check:id = 1" json:"-"`
	Version              uint64    `gorm:"not null" json:"version"`
	Enabled              bool      `gorm:"not null" json:"enabled"`
	Host                 string    `gorm:"not null;size:255" json:"host"`
	Port                 int       `gorm:"not null" json:"port"`
	TLSMode              string    `gorm:"not null;size:20" json:"tls_mode"`
	FromAddress          string    `gorm:"not null;size:320" json:"from_address"`
	FromName             string    `gorm:"not null;size:255" json:"from_name"`
	Username             string    `gorm:"not null;size:255" json:"username"`
	CredentialCiphertext string    `gorm:"type:text" json:"-"`
	CredentialVersion    uint64    `gorm:"not null" json:"-"`
	UpdatedBy            uint      `gorm:"not null" json:"updated_by"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

func (SMTPRelay) TableName() string { return "monitor.smtp_relay" }
