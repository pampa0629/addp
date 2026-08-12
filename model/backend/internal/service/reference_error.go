package service

import (
	"errors"
	"net/http"

	commonclient "github.com/addp/common/client"
	"github.com/addp/model/i18n"
	"github.com/addp/model/internal/apperrors"
)

func standardReferenceError(err error, code string) error {
	var transportError *commonclient.TenantTransportError
	if errors.As(err, &transportError) {
		return apperrors.Wrap(apperrors.KindUnavailable, "standard_service_unavailable", i18n.MsgStandardUnavailable, err)
	}
	if status, ok := commonclient.TenantAPIStatusCode(err); ok {
		if status == http.StatusNotFound {
			return apperrors.Wrap(apperrors.KindNotFound, code, i18n.MsgReferenceNotFound, err)
		}
		if status >= http.StatusInternalServerError {
			return apperrors.Wrap(apperrors.KindUnavailable, "standard_service_unavailable", i18n.MsgStandardUnavailable, err)
		}
	}
	if errors.Is(err, commonclient.ErrTenantReferenceNotFound) {
		return apperrors.Wrap(apperrors.KindNotFound, code, i18n.MsgReferenceNotFound, err)
	}
	return err
}
