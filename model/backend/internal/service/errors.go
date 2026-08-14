package service

import (
	"errors"

	commonapi "github.com/addp/common/api"
	"github.com/addp/model/i18n"
	"github.com/addp/model/internal/apperrors"
)

func resourceVersionConflict() error {
	return apperrors.Conflict("resource_version_conflict", i18n.MsgResourceVersionConflict)
}

func requireVersion(current, requested int64) error {
	if requested <= 0 {
		return invalidRequest()
	}
	if current != requested {
		return resourceVersionConflict()
	}
	return nil
}

func modelResourceError(err error, code, messageID string) error {
	if errors.Is(err, commonapi.ErrNotFound) {
		return apperrors.Wrap(apperrors.KindNotFound, code, messageID, err)
	}
	if errors.Is(err, commonapi.ErrConflict) {
		return apperrors.Wrap(apperrors.KindConflict, code+"_conflict", messageID, err)
	}
	return err
}
