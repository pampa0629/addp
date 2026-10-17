package dataprotection

import (
	"errors"
	"github.com/addp/common/models"
	"strings"
)

// VerifyTargetEngine binds Security's trusted enrollment preimage to the
// DataItem identity. Consumer input and engine type are not ownership facts.
func VerifyTargetEngine(target ResourceReference, engineID uint, fullName string) error {
	if target.ResourceType != "data_item" {
		if engineID != 0 {
			return errors.New("professional resource cannot declare a data engine")
		}
		return nil
	}
	if engineID == 0 || strings.TrimSpace(fullName) == "" || models.GenerateItemFingerprint(engineID, fullName) != target.ResourceIdentity {
		return errors.New("protection target engine preimage is missing or inconsistent")
	}
	return nil
}
