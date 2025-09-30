package models

import (
	"time"
)

type User struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Username     string    `gorm:"uniqueIndex;not null" json:"username"`
	Email        string    `gorm:"uniqueIndex" json:"email"`
	PasswordHash string    `gorm:"not null" json:"-"`
	FullName     string    `json:"full_name"`
	IsActive     bool      `gorm:"default:true" json:"is_active"`
	IsSuperuser  bool      `gorm:"default:false" json:"is_superuser"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type UserCreateRequest struct {
	Username    string `json:"username" binding:"required"`
	Email       string `json:"email"`
	Password    string `json:"password" binding:"required,min=6"`
	FullName    string `json:"full_name"`
	IsSuperuser bool   `json:"is_superuser"`
}

type UserUpdateRequest struct {
	Email       *string `json:"email"`
	FullName    *string `json:"full_name"`
	Password    *string `json:"password"`
	IsActive    *bool   `json:"is_active"`
	IsSuperuser *bool   `json:"is_superuser"`
}

type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type LoginResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
}