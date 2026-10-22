package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/addp/common/jsonmap"
	"github.com/google/uuid"
)

// executionAuthorizationJSON is the one bounded fixed-purpose EA transport:
// no redirects, hidden POST retries, legacy parsing or upstream body leakage.
func (c *SystemServiceClient) executionAuthorizationJSON(ctx context.Context, path, credential string, request, response any, wantStatus int) error {
	return c.boundedAuthorizationJSON(ctx, http.MethodPost, path, credential, request, response, wantStatus)
}

func (c *SystemServiceClient) boundedAuthorizationJSON(ctx context.Context, method, path, credential string, request, response any, wantStatus int) error {
	if c == nil || c.httpClient == nil || c.baseURL == "" {
		return errors.New("execution authorization System endpoint unavailable")
	}
	bound := *c
	transport := *c.httpClient
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	bound.httpClient = &transport
	var raw json.RawMessage
	status, err := bound.doJSON(ctx, method, path, credential, request, &raw, 512<<10)
	if err != nil || status != wantStatus {
		code := "execution_authorization_internal_error"
		switch status {
		case 400:
			code = "invalid_execution_authorization_request"
		case 401:
			code = "authentication_required"
		case 403:
			code = "permission_denied"
		case 409:
			code = "execution_authorization_conflict"
		}
		var apiError *SystemAPIError
		if errors.As(err, &apiError) && apiError.ErrorCode == "execution_authorization_unavailable" {
			code = apiError.ErrorCode
		}
		return &SystemAPIError{Method: method, Path: path, StatusCode: status, ErrorCode: code}
	}
	if _, err := jsonmap.DecodeObjectStrict(raw, 512<<10, 64); err != nil {
		return errors.New("invalid execution authorization System response")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(response) != nil || decoder.Decode(new(any)) != io.EOF {
		return errors.New("invalid execution authorization System response")
	}
	return nil
}

func canonicalExecutionUUID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil && id.String() == value
}

func validExecutionBearer(credential string) bool {
	return strings.HasPrefix(credential, "addp_at_") && len(credential) > len("addp_at_") && !strings.ContainsAny(credential, " \t\r\n")
}
