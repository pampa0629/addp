package models

import "time"

// UserType 用户类型
type UserType string

const (
	UserTypeSuperAdmin  UserType = "super_admin"  // 超级管理员
	UserTypeTenantAdmin UserType = "tenant_admin" // 租户管理员
	UserTypeUser        UserType = "user"         // 普通用户
)

// User 用户模型（共享定义）
type User struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Username     string    `gorm:"not null;unique" json:"username"`
	Email        string    `gorm:"unique" json:"email"`
	PasswordHash string    `gorm:"not null" json:"-"`
	FullName     string    `json:"full_name"`
	IsActive     bool      `gorm:"default:true" json:"is_active"`
	UserType     UserType  `gorm:"type:varchar(20);default:'user';not null" json:"user_type"` // 用户类型
	TenantID     *uint     `gorm:"index" json:"tenant_id"`                                    // 租户ID (SuperAdmin没有租户)
	CreatedAt    time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// 实现 UserInterface 接口
func (u *User) GetID() uint {
	return u.ID
}

func (u *User) GetUserType() string {
	return string(u.UserType)
}

func (u *User) GetTenantID() *uint {
	return u.TenantID
}
