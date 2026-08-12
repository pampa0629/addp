package service

import (
	"errors"

	commonapi "github.com/addp/common/api"
	"github.com/addp/model/internal/apperrors"
)

func modelResourceError(err error, code, messageID string) error {
	if errors.Is(err, commonapi.ErrNotFound) {
		return apperrors.Wrap(apperrors.KindNotFound, code, messageID, err)
	}
	if errors.Is(err, commonapi.ErrConflict) {
		return apperrors.Wrap(apperrors.KindConflict, code+"_conflict", messageID, err)
	}
	return err
}
