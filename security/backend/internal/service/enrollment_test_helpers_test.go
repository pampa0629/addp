package service

import (
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/security/internal/models"
)

func testDataItemEnrollmentRequest(engineID uint, itemType resourcetree.ResourceType, fullName string) models.CreateProtectionEnrollmentRequest {
	itemID := uint(51657)
	locator := resourcetree.LocatorFromFullName(engineID, "", string(itemType), fullName, &itemID)
	if locator == nil {
		panic("invalid test DataItem locator")
	}
	return models.CreateProtectionEnrollmentRequest{Locator: locator.ToURI()}
}

func testDataItemFingerprint(engineID uint, fullName string) string {
	return commonmodels.GenerateItemFingerprint(engineID, fullName)
}
