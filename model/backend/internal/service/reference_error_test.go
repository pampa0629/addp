package service

import (
	"errors"
	"net/http"
	"testing"

	commonclient "github.com/addp/common/client"
	"github.com/addp/model/internal/apperrors"
)

func TestStandardReferenceErrorMapsOnlyNotFound(t *testing.T) {
	notFound := &commonclient.TenantAPIError{StatusCode: http.StatusNotFound, ErrorCode: "domain_not_found"}
	err := standardReferenceError(notFound, "domain_not_found")
	domainErr, ok := apperrors.As(err)
	if !ok || domainErr.Kind != apperrors.KindNotFound || domainErr.Code != "domain_not_found" {
		t.Fatalf("standardReferenceError() = %#v, %t", domainErr, ok)
	}

	serviceUnavailable := &commonclient.TenantAPIError{StatusCode: http.StatusServiceUnavailable}
	err = standardReferenceError(serviceUnavailable, "domain_not_found")
	domainErr, ok = apperrors.As(err)
	if !ok || domainErr.Kind != apperrors.KindUnavailable || domainErr.Code != "standard_service_unavailable" || !errors.Is(err, serviceUnavailable) {
		t.Fatalf("service unavailable error = %#v, %t", domainErr, ok)
	}

	forbidden := &commonclient.TenantAPIError{StatusCode: http.StatusForbidden, ErrorCode: "permission_denied"}
	err = standardReferenceError(forbidden, "domain_not_found")
	domainErr, ok = apperrors.As(err)
	if !ok || domainErr.Kind != apperrors.KindUnavailable || domainErr.Code != "standard_service_unavailable" || !errors.Is(err, forbidden) {
		t.Fatalf("forbidden upstream error = %#v, %t", domainErr, ok)
	}

	crossTenant := commonclient.ErrTenantReferenceNotFound
	err = standardReferenceError(crossTenant, "domain_not_found")
	domainErr, ok = apperrors.As(err)
	if !ok || domainErr.Kind != apperrors.KindNotFound || !errors.Is(err, crossTenant) {
		t.Fatalf("cross-tenant error = %#v, %t", domainErr, ok)
	}

	transportFailure := &commonclient.TenantTransportError{Cause: errors.New("connection refused")}
	err = standardReferenceError(transportFailure, "domain_not_found")
	domainErr, ok = apperrors.As(err)
	if !ok || domainErr.Kind != apperrors.KindUnavailable || !errors.Is(err, transportFailure) {
		t.Fatalf("transport failure = %#v, %t", domainErr, ok)
	}

	tokenFailure := errors.New("service token unavailable")
	err = standardReferenceError(tokenFailure, "domain_not_found")
	domainErr, ok = apperrors.As(err)
	if !ok || domainErr.Kind != apperrors.KindUnavailable || domainErr.Code != "standard_service_unavailable" || !errors.Is(err, tokenFailure) {
		t.Fatalf("token failure = %#v, %t", domainErr, ok)
	}
}
