package models

// UserInterface 用户接口
// 避免 common 模块直接依赖具体的 User 模型
type UserInterface interface {
	GetID() uint
	GetUserType() string
	GetTenantID() *uint
}
