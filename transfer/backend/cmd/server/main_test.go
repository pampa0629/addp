package main

import "testing"

func TestTransferSchemaModelsIncludeDeadLetters(t *testing.T) {
	tableNames := make(map[string]struct{})
	for _, model := range transferSchemaModels() {
		table, ok := model.(interface{ TableName() string })
		if !ok {
			t.Fatalf("transfer schema model %T does not declare TableName", model)
		}
		tableNames[table.TableName()] = struct{}{}
	}
	if _, ok := tableNames["transfer.dead_letters"]; !ok {
		t.Fatal("transfer.dead_letters is missing from the server AutoMigrate models")
	}
}
