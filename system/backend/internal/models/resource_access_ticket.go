package models

import "time"

const (
	BrowserResourceAccessTicketCookieName = "addp_resource_access_ticket"
	BrowserResourceAccessScope            = "resource:read"
)

var BrowserResourceAccessOwners = []string{"manager", "standard"}

type ResourceAccessTicket struct {
	ID        string     `gorm:"primaryKey;type:uuid" json:"id"`
	TokenHash string     `gorm:"type:char(64);not null;uniqueIndex" json:"-"`
	FamilyID  string     `gorm:"type:uuid;not null;index" json:"family_id"`
	Owner     string     `gorm:"type:varchar(100);not null;index" json:"owner"`
	ExpiresAt time.Time  `gorm:"not null;index" json:"expires_at"`
	RevokedAt *time.Time `gorm:"index" json:"revoked_at"`
	CreatedAt time.Time  `gorm:"autoCreateTime" json:"created_at"`
}

func (ResourceAccessTicket) TableName() string { return "system.resource_access_tickets" }
