package service_test

import (
	"testing"

	"github.com/addp/common/models"
	commonservice "github.com/addp/common/service"
	"github.com/stretchr/testify/assert"
)

// MockUser 用于测试的模拟用户
type MockUser struct {
	ID       uint
	UserType string
	TenantID *uint
}

func (m *MockUser) GetID() uint          { return m.ID }
func (m *MockUser) GetUserType() string  { return m.UserType }
func (m *MockUser) GetTenantID() *uint   { return m.TenantID }

func TestAuthService_IsSuperAdmin(t *testing.T) {
	authSvc := commonservice.NewAuthService()

	tests := []struct {
		name     string
		user     models.UserInterface
		expected bool
	}{
		{
			name:     "SuperAdmin user",
			user:     &MockUser{ID: 1, UserType: "super_admin"},
			expected: true,
		},
		{
			name:     "TenantAdmin user",
			user:     &MockUser{ID: 2, UserType: "tenant_admin"},
			expected: false,
		},
		{
			name:     "Normal user",
			user:     &MockUser{ID: 3, UserType: "user"},
			expected: false,
		},
		{
			name:     "Nil user",
			user:     nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := authSvc.IsSuperAdmin(tt.user)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAuthService_IsTenantAdmin(t *testing.T) {
	authSvc := commonservice.NewAuthService()

	tests := []struct {
		name     string
		user     models.UserInterface
		expected bool
	}{
		{
			name:     "TenantAdmin user",
			user:     &MockUser{ID: 1, UserType: "tenant_admin"},
			expected: true,
		},
		{
			name:     "SuperAdmin user",
			user:     &MockUser{ID: 2, UserType: "super_admin"},
			expected: false,
		},
		{
			name:     "Normal user",
			user:     &MockUser{ID: 3, UserType: "user"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := authSvc.IsTenantAdmin(tt.user)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAuthService_CheckTenantAccess(t *testing.T) {
	authSvc := commonservice.NewAuthService()

	tenantID1 := uint(1)
	tenantID2 := uint(2)

	tests := []struct {
		name             string
		user             models.UserInterface
		resourceTenantID *uint
		expectError      bool
		errorContains    string
	}{
		{
			name:             "SuperAdmin can access any tenant",
			user:             &MockUser{ID: 1, UserType: "super_admin", TenantID: &tenantID1},
			resourceTenantID: &tenantID2,
			expectError:      false,
		},
		{
			name:             "TenantAdmin can access same tenant",
			user:             &MockUser{ID: 2, UserType: "tenant_admin", TenantID: &tenantID1},
			resourceTenantID: &tenantID1,
			expectError:      false,
		},
		{
			name:             "TenantAdmin cannot access other tenant",
			user:             &MockUser{ID: 3, UserType: "tenant_admin", TenantID: &tenantID1},
			resourceTenantID: &tenantID2,
			expectError:      true,
			errorContains:    "没有权限访问该租户的资源",
		},
		{
			name:             "Normal user can access same tenant",
			user:             &MockUser{ID: 4, UserType: "user", TenantID: &tenantID1},
			resourceTenantID: &tenantID1,
			expectError:      false,
		},
		{
			name:             "Nil user returns error",
			user:             nil,
			resourceTenantID: &tenantID1,
			expectError:      true,
			errorContains:    "用户不存在",
		},
		{
			name:             "Nil tenant ID returns error",
			user:             &MockUser{ID: 5, UserType: "user", TenantID: nil},
			resourceTenantID: &tenantID1,
			expectError:      true,
			errorContains:    "租户信息不完整",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := authSvc.CheckTenantAccess(tt.user, tt.resourceTenantID)
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestAuthService_CheckResourceOwnership(t *testing.T) {
	authSvc := commonservice.NewAuthService()

	creatorID1 := uint(1)
	creatorID2 := uint(2)

	tests := []struct {
		name          string
		user          models.UserInterface
		creatorID     *uint
		expectError   bool
		errorContains string
	}{
		{
			name:        "SuperAdmin can manage any resource",
			user:        &MockUser{ID: 1, UserType: "super_admin"},
			creatorID:   &creatorID2,
			expectError: false,
		},
		{
			name:        "TenantAdmin can manage any resource",
			user:        &MockUser{ID: 2, UserType: "tenant_admin"},
			creatorID:   &creatorID2,
			expectError: false,
		},
		{
			name:        "Normal user can manage own resource",
			user:        &MockUser{ID: 1, UserType: "user"},
			creatorID:   &creatorID1,
			expectError: false,
		},
		{
			name:          "Normal user cannot manage others' resource",
			user:          &MockUser{ID: 1, UserType: "user"},
			creatorID:     &creatorID2,
			expectError:   true,
			errorContains: "只能管理自己创建的资源",
		},
		{
			name:          "Nil creator ID returns error",
			user:          &MockUser{ID: 1, UserType: "user"},
			creatorID:     nil,
			expectError:   true,
			errorContains: "只能管理自己创建的资源",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := authSvc.CheckResourceOwnership(tt.user, tt.creatorID)
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestAuthService_CheckCreatePermission(t *testing.T) {
	authSvc := commonservice.NewAuthService()

	tests := []struct {
		name          string
		user          models.UserInterface
		resourceType  string
		expectError   bool
		errorContains string
	}{
		{
			name:         "SuperAdmin can create any resource",
			user:         &MockUser{ID: 1, UserType: "super_admin"},
			resourceType: "engine",
			expectError:  false,
		},
		{
			name:         "TenantAdmin can create any resource",
			user:         &MockUser{ID: 2, UserType: "tenant_admin"},
			resourceType: "engine",
			expectError:  false,
		},
		{
			name:         "Normal user can create profile",
			user:         &MockUser{ID: 3, UserType: "user"},
			resourceType: "profile",
			expectError:  false,
		},
		{
			name:          "Normal user cannot create engine",
			user:          &MockUser{ID: 4, UserType: "user"},
			resourceType:  "engine",
			expectError:   true,
			errorContains: "没有权限创建此类资源",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := authSvc.CheckCreatePermission(tt.user, tt.resourceType)
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestAuthService_CheckDeletePermission(t *testing.T) {
	authSvc := commonservice.NewAuthService()

	tenantID1 := uint(1)
	tenantID2 := uint(2)
	creatorID1 := uint(1)

	tests := []struct {
		name             string
		user             models.UserInterface
		creatorID        *uint
		resourceTenantID *uint
		isBuiltin        bool
		expectError      bool
		errorContains    string
	}{
		{
			name:             "Cannot delete builtin resource",
			user:             &MockUser{ID: 1, UserType: "super_admin"},
			creatorID:        &creatorID1,
			resourceTenantID: &tenantID1,
			isBuiltin:        true,
			expectError:      true,
			errorContains:    "内置资源不可删除",
		},
		{
			name:             "SuperAdmin can delete any non-builtin resource",
			user:             &MockUser{ID: 1, UserType: "super_admin", TenantID: &tenantID1},
			creatorID:        &creatorID1,
			resourceTenantID: &tenantID2,
			isBuiltin:        false,
			expectError:      false,
		},
		{
			name:             "TenantAdmin can delete same tenant resource",
			user:             &MockUser{ID: 2, UserType: "tenant_admin", TenantID: &tenantID1},
			creatorID:        &creatorID1,
			resourceTenantID: &tenantID1,
			isBuiltin:        false,
			expectError:      false,
		},
		{
			name:             "TenantAdmin cannot delete other tenant resource",
			user:             &MockUser{ID: 3, UserType: "tenant_admin", TenantID: &tenantID1},
			creatorID:        &creatorID1,
			resourceTenantID: &tenantID2,
			isBuiltin:        false,
			expectError:      true,
			errorContains:    "没有权限访问该租户的资源",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := authSvc.CheckDeletePermission(tt.user, tt.creatorID, tt.resourceTenantID, tt.isBuiltin)
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
