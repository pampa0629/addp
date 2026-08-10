package api

import "testing"

func TestErrorResponseIncludesStableCode(t *testing.T) {
	response := errorResponse("localized message")
	if response["error"] != "localized message" || response["error_code"] != "model_request_failed" {
		t.Fatalf("unexpected error response: %#v", response)
	}
}
