package models

import (
	"time"
)

// UserType 用户类型
type UserType string

const (
	UserTypeSuperAdmin  UserType = "super_admin"  // 超级管理员
	UserTypeTenantAdmin UserType = "tenant_admin" // 租户管理员
	UserTypeUser        UserType = "user"         // 普通用户
)

type User struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Username     string    `gorm:"uniqueIndex;not null" json:"username"`
	Email        string    `gorm:"uniqueIndex" json:"email"`
	PasswordHash string    `gorm:"not null" json:"-"`
	FullName     string    `json:"full_name"`
	IsActive     bool      `gorm:"default:true" json:"is_active"`
	UserType     UserType  `gorm:"type:varchar(20);default:'user';not null" json:"user_type"` // 用户类型
	TenantID     *uint     `gorm:"index" json:"tenant_id"`                                     // 租户ID (SuperAdmin没有租户)
	IsSuperuser  bool      `gorm:"default:false" json:"is_superuser"`                          // 保留以兼容旧代码
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type UserCreateRequest struct {
	Username string   `json:"username" binding:"required"`
	Email    string   `json:"email"`
	Password string   `json:"password" binding:"required,min=6"`
	FullName string   `json:"full_name"`
	UserType UserType `json:"user_type"` // 用户类型
}

type UserUpdateRequest struct {
	Email    *string   `json:"email"`
	FullName *string   `json:"full_name"`
	Password *string   `json:"password"`
	IsActive *bool     `json:"is_active"`
	UserType *UserType `json:"user_type"` // 用户类型
}

type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type LoginResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
}