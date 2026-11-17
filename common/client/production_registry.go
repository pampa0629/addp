package client

import "github.com/addp/common/production"

// NewProductionRegistry is the only production-owner composition shared by
// System, Meta Backend and Meta Worker. Permission definitions and role grants
// remain in their owner manifests; this function grants no authority.
func NewProductionRegistry(transfer *TransferClient, develop *DevelopClient) (*production.Registry, error) {
	return production.NewRegistry(
		production.Owner{Module: "transfer", ClientID: "addp-transfer", ReceiptPermission: "transfer.production_receipt.read", ExecutionPermission: "transfer.task.execute", ReadReceipt: transfer.ReadCommittedProduction},
		production.Owner{Module: "develop", ClientID: "addp-develop", ReceiptPermission: "develop.production_receipt.read", ExecutionPermission: "develop.task.execute", ReadReceipt: develop.ReadCommittedProduction},
	)
}
