package engineaccess

import (
	"errors"

	engineselection "github.com/addp/common/engine/selection"
	commonModels "github.com/addp/common/models"
)

var ErrUnavailable = errors.New("engine is unavailable")

// EnsureAvailable verifies the current System-owned lifecycle and connectivity facts.
// Callers must fetch a fresh Engine before invoking this function.
func EnsureAvailable(engine *commonModels.Engine) error {
	if !engineselection.IsAvailable(engine) {
		return ErrUnavailable
	}
	return nil
}
