package models

import (
	commonmodels "github.com/addp/common/models"
)

// 使用 common/models 中的类型定义
type (
	UserType = commonmodels.UserType
	User     = commonmodels.User
)

// 常量定义
const (
	UserTypeSuperAdmin  = commonmodels.UserTypeSuperAdmin
	UserTypeTenantAdmin = commonmodels.UserTypeTenantAdmin
	UserTypeUser        = commonmodels.UserTypeUser
)

// UserCreateRequest DTO - 保留在 system 模块中
type UserCreateRequest struct {
	Username string   `json:"username" binding:"required"`
	Email    string   `json:"email"`
	Password string   `json:"password" binding:"required,min=6"`
	FullName string   `json:"full_name"`
	UserType UserType `json:"user_type"` // 用户类型
}

// UserUpdateRequest DTO
type UserUpdateRequest struct {
	Email    *string   `json:"email"`
	FullName *string   `json:"full_name"`
	Password *string   `json:"password"`
	IsActive *bool     `json:"is_active"`
	UserType *UserType `json:"user_type"` // 用户类型
}

// LoginRequest DTO
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// LoginResponse DTO
type LoginResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
}

// ChangePasswordRequest DTO
type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=6"`
}
