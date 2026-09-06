package service

const (
	managerProtectionOwner  = "manager"
	developProtectionOwner  = "develop"
	serviceProtectionOwner  = "service"
	transferProtectionOwner = "transfer"

	managerPreviewAction     = "preview"
	managerProfileAction     = "profile"
	managerSearchIndexAction = "search_index"
	developQueryAction       = "query"
	serviceExecuteAction     = "service_execute"
	transferExportAction     = "export"
)

type protectionOwnerContract struct {
	owner           string
	exemptionAction string
}

// requiredProtectionOwnerContracts is the single Security-owned source of
// truth for enrollment activation owners and their exemptible primary action.
// Keep the order stable because projection changes are append-only.
var requiredProtectionOwnerContracts = [...]protectionOwnerContract{
	{owner: developProtectionOwner, exemptionAction: developQueryAction},
	{owner: managerProtectionOwner, exemptionAction: managerPreviewAction},
	{owner: serviceProtectionOwner, exemptionAction: serviceExecuteAction},
	{owner: transferProtectionOwner, exemptionAction: transferExportAction},
}

func allRequiredProtectionOwners() []string {
	owners := make([]string, 0, len(requiredProtectionOwnerContracts))
	for _, contract := range requiredProtectionOwnerContracts {
		owners = append(owners, contract.owner)
	}
	return owners
}

func isRequiredOwner(owner string) bool {
	_, exists := requiredProtectionOwnerContract(owner)
	return exists
}

func validExemptionBinding(owner, action string) bool {
	contract, exists := requiredProtectionOwnerContract(owner)
	return exists && action == contract.exemptionAction
}

func requiredProtectionOwnerContract(owner string) (protectionOwnerContract, bool) {
	for _, contract := range requiredProtectionOwnerContracts {
		if contract.owner == owner {
			return contract, true
		}
	}
	return protectionOwnerContract{}, false
}
