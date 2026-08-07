package service

import (
	"context"

	commonClient "github.com/addp/common/client"
)

func newTestSystemClient(baseURL string) *commonClient.SystemClient {
	return commonClient.NewSystemClient(baseURL, commonClient.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
		return "addp_at_test_service_token", nil
	}))
}
