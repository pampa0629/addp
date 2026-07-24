package api

import (
	"fmt"
	"net/http"
	"testing"
)

func TestRouterPublishesOnlyImplementedTypeDefinitionOperations(t *testing.T) {
	router := SetupRouter(nil, "http://system", nil, nil)
	routes := make(map[string]struct{})
	for _, route := range router.Routes() {
		routes[fmt.Sprintf("%s %s", route.Method, route.Path)] = struct{}{}
	}

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		key := fmt.Sprintf("%s /api/v1/asset/type-definitions", method)
		if method != http.MethodPost {
			key += "/:id"
		}
		if _, exists := routes[key]; exists {
			t.Fatalf("unsupported route remains published: %s", key)
		}
	}

	publicBusinessRoutes := 0
	for _, route := range router.Routes() {
		if len(route.Path) >= len("/api/v1/asset") && route.Path[:len("/api/v1/asset")] == "/api/v1/asset" {
			publicBusinessRoutes++
		}
	}
	if publicBusinessRoutes != 34 {
		t.Fatalf("public business route count = %d, want 34", publicBusinessRoutes)
	}
}
