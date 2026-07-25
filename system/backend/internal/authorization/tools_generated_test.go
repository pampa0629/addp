package authorization

import "testing"

func TestToolAuthorizationCatalogReturnsDetachedValues(t *testing.T) {
	catalog := ToolAuthorizationCatalog{}
	tool, ok := catalog.FindToolAuthorization("workflow.run")
	if !ok || tool.Owner != "develop" || len(tool.RequiredScopes) != 1 ||
		len(tool.RequiredPermissions) != 1 || tool.RequiredPermissions[0] != "develop.task.execute" {
		t.Fatalf("workflow.run authorization = %#v, found=%v", tool, ok)
	}
	tool.RequiredScopes[0] = "modified.scope"
	tool.RequiredPermissions[0] = "modified.permission"

	reloaded, ok := catalog.FindToolAuthorization("workflow.run")
	if !ok || reloaded.RequiredScopes[0] != "workflow.run" ||
		reloaded.RequiredPermissions[0] != "develop.task.execute" {
		t.Fatalf("Tool catalog was mutated through returned slices: %#v", reloaded)
	}
	if _, ok := catalog.FindToolAuthorization("workflow.unknown"); ok {
		t.Fatal("unknown Tool was found")
	}
}
