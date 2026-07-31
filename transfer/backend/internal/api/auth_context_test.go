package api

import (
	"strconv"
	"testing"

	"github.com/addp/common/authtest"
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
)

func setTransferTestAuthContext(t *testing.T, c *gin.Context, tenantID, userID uint) {
	t.Helper()
	tenantIDText := strconv.FormatUint(uint64(tenantID), 10)
	userIDText := strconv.FormatUint(uint64(userID), 10)
	authContext := authtest.NewTenantUserAuthContext(tenantIDText, userIDText, []string{"transfer.task.read"})
	if err := commonAuth.SetAuthContextForGin(c, authContext); err != nil {
		t.Fatalf("set canonical AuthContext: %v", err)
	}
}
