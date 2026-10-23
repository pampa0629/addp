package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/jsonmap"
)

// TransferSourceProtectionSelection contains references only, never role facts.
type TransferSourceProtectionSelection struct {
	SourceLocator string                           `json:"source_locator"`
	Choices       []dataprotection.FieldRoleChoice `json:"protection_choices"`
}

type TransferProtectionOptions struct {
	Fields []dataprotection.FieldRoleOptions `json:"fields"`
}

func ValidateTransferUserAuthorization(a *IssuedExecutionAuthorization, tenant, user uint, executionID string) error {
	invalid := dataprotection.ErrRoleOptionIneligible
	if a == nil || a.InternalTask != nil || a.SourceType != "user" || a.SourceDefinitionID != nil || a.SourceDefinitionVersion != nil || a.Audience != "transfer" || a.ExecutionID != executionID || !validExportExecutionID(executionID) || tenant == 0 || user == 0 || a.TenantID != strconv.FormatUint(uint64(tenant), 10) || a.ActorPrincipalID != strconv.FormatUint(uint64(user), 10) || !a.ExpiresAt.After(time.Now().UTC()) {
		return invalid
	}
	for _, id := range []string{a.ID, a.TenantMembershipID, a.IssuedAuthorizationVersion} {
		if _, err := parseCanonicalPositiveID(id); err != nil {
			return invalid
		}
	}
	return nil
}

// These two methods are synchronous BFF calls with the current User credential.
// They never use this client's Service credential or follow redirects.
func (c *TransferClient) SourceProtectionOptions(ctx context.Context, bearer, locator string) (*TransferProtectionOptions, error) {
	var result TransferProtectionOptions
	if err := c.userProtectionJSON(ctx, http.MethodGet, "/api/v1/transfer/source-protection-options?source_locator="+url.QueryEscape(locator), bearer, nil, &result); err != nil {
		return nil, err
	}
	if result.Fields == nil {
		return nil, dataprotection.ErrRoleOptionUnavailable
	}
	return &result, nil
}
func (c *TransferClient) ValidateSourceProtection(ctx context.Context, bearer string, selection *TransferSourceProtectionSelection) error {
	if selection == nil || selection.SourceLocator == "" || len(selection.Choices) == 0 || dataprotection.ValidateFieldRoleChoices(selection.Choices) != nil {
		return dataprotection.ErrRoleOptionUnavailable
	}
	var result struct {
		Valid bool `json:"valid"`
	}
	if err := c.userProtectionJSON(ctx, http.MethodPost, "/api/v1/transfer/source-protection-checks", bearer, selection, &result); err != nil {
		return err
	}
	if !result.Valid {
		return dataprotection.ErrRoleOptionUnavailable
	}
	return nil
}
func (c *TransferClient) userProtectionJSON(ctx context.Context, method, path, bearer string, body, result any) error {
	if c == nil || c.baseURL == "" {
		return dataprotection.ErrRoleOptionUnavailable
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return dataprotection.ErrRoleOptionUnavailable
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.doUserTaskRequest(req, bearer)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		switch resp.StatusCode {
		case 401:
			return commonAPI.ErrUnauthorized
		case 403:
			return commonAPI.ErrForbidden
		case 400, 409:
			return dataprotection.ErrRoleOptionUnavailable
		default:
			return errors.New("Transfer protection verification unavailable")
		}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (256<<10)+1))
	if err != nil {
		return dataprotection.ErrRoleOptionUnavailable
	}
	if _, err := jsonmap.DecodeObjectStrict(raw, 256<<10, 32); err != nil {
		return dataprotection.ErrRoleOptionUnavailable
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(result) != nil || decoder.Decode(new(any)) != io.EOF {
		return dataprotection.ErrRoleOptionUnavailable
	}
	return nil
}
