package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	commonapi "github.com/addp/common/api"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestBindIDParam(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		paramValue  string
		expectError bool
		expectedID  uint
	}{
		{
			name:        "Valid ID",
			paramValue:  "123",
			expectError: false,
			expectedID:  123,
		},
		{
			name:        "Valid ID - zero",
			paramValue:  "0",
			expectError: false,
			expectedID:  0,
		},
		{
			name:        "Invalid ID - not a number",
			paramValue:  "abc",
			expectError: true,
		},
		{
			name:        "Invalid ID - negative number",
			paramValue:  "-1",
			expectError: true,
		},
		{
			name:        "Invalid ID - too large",
			paramValue:  "99999999999999999999",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Params = gin.Params{
				{Key: "id", Value: tt.paramValue},
			}

			id, err := commonapi.BindIDParam(c, "id")

			if tt.expectError {
				assert.Error(t, err)
				assert.Equal(t, http.StatusBadRequest, w.Code)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedID, id)
			}
		})
	}
}

func TestGetCurrentUserID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		setUserID   bool
		userID      uint
		expectFound bool
	}{
		{
			name:        "User ID exists",
			setUserID:   true,
			userID:      123,
			expectFound: true,
		},
		{
			name:        "User ID not set",
			setUserID:   false,
			expectFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			if tt.setUserID {
				c.Set("user_id", tt.userID)
			}

			id, found := commonapi.GetCurrentUserID(c)

			assert.Equal(t, tt.expectFound, found)
			if tt.expectFound {
				assert.Equal(t, tt.userID, id)
			}
		})
	}
}

func TestMustGetCurrentUserID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		setUserID   bool
		userID      uint
		expectError bool
	}{
		{
			name:        "User ID exists",
			setUserID:   true,
			userID:      123,
			expectError: false,
		},
		{
			name:        "User ID not set",
			setUserID:   false,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			if tt.setUserID {
				c.Set("user_id", tt.userID)
			}

			id, err := commonapi.MustGetCurrentUserID(c)

			if tt.expectError {
				assert.Error(t, err)
				assert.Equal(t, http.StatusUnauthorized, w.Code)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.userID, id)
			}
		})
	}
}

func TestParsePagination(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name         string
		pageQuery    string
		sizeQuery    string
		expectedPage int
		expectedSize int
	}{
		{
			name:         "Valid pagination",
			pageQuery:    "2",
			sizeQuery:    "20",
			expectedPage: 2,
			expectedSize: 20,
		},
		{
			name:         "Default values",
			pageQuery:    "",
			sizeQuery:    "",
			expectedPage: 1,
			expectedSize: 10,
		},
		{
			name:         "Invalid page - use default",
			pageQuery:    "abc",
			sizeQuery:    "20",
			expectedPage: 1,
			expectedSize: 20,
		},
		{
			name:         "Page less than 1 - use default",
			pageQuery:    "0",
			sizeQuery:    "20",
			expectedPage: 1,
			expectedSize: 20,
		},
		{
			name:         "Size less than 1 - use default",
			pageQuery:    "2",
			sizeQuery:    "0",
			expectedPage: 2,
			expectedSize: 10,
		},
		{
			name:         "Size greater than 100 - cap at 100",
			pageQuery:    "1",
			sizeQuery:    "200",
			expectedPage: 1,
			expectedSize: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/?page="+tt.pageQuery+"&page_size="+tt.sizeQuery, nil)

			page, size := commonapi.ParsePagination(c)

			assert.Equal(t, tt.expectedPage, page)
			assert.Equal(t, tt.expectedSize, size)
		})
	}
}

func TestRespondSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	data := map[string]string{"message": "success"}
	commonapi.RespondSuccess(c, data)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "success")
}

func TestRespondCreated(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	data := map[string]string{"id": "123"}
	commonapi.RespondCreated(c, data)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), "123")
}

func TestRespondError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		statusCode int
		message    string
	}{
		{
			name:       "Bad Request",
			statusCode: http.StatusBadRequest,
			message:    "Invalid input",
		},
		{
			name:       "Unauthorized",
			statusCode: http.StatusUnauthorized,
			message:    "Unauthorized access",
		},
		{
			name:       "Not Found",
			statusCode: http.StatusNotFound,
			message:    "Resource not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			commonapi.RespondError(c, tt.statusCode, tt.message)

			assert.Equal(t, tt.statusCode, w.Code)
			assert.Contains(t, w.Body.String(), tt.message)
		})
	}
}
