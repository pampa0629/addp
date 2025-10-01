package models

import (
	"time"
)

type Tenant struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"uniqueIndex;not null" json:"name"`
	Description string    `json:"description"`
	IsActive    bool      `gorm:"default:true" json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type TenantCreateRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
	AdminUsername string `json:"admin_username" binding:"required"`
	AdminPassword string `json:"admin_password" binding:"required,min=6"`
	AdminEmail    string `json:"admin_email"`
	AdminFullName string `json:"admin_full_name"`
}

type TenantUpdateRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	IsActive    *bool   `json:"is_active"`
}
