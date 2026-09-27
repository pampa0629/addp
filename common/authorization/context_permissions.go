package authorization

// HasContextPermissions checks functional permissions only from Assignments
// covering the active platform or tenant context. Resource owner decisions are
// separate and may further restrict access.
func HasContextPermissions(authContext AuthContext, requiredPermissions ...string) bool {
	if len(requiredPermissions) == 0 {
		return false
	}
	required := make(map[string]struct{}, len(requiredPermissions))
	for _, permission := range requiredPermissions {
		if ValidatePermissionKey(permission) != nil {
			return false
		}
		required[permission] = struct{}{}
	}
	for _, assignment := range authContext.Authorization.RoleAssignments {
		if !assignmentCoversContext(assignment.Scope, authContext.Context) {
			continue
		}
		for _, permission := range assignment.Permissions {
			delete(required, permission)
		}
	}
	return len(required) == 0
}

func assignmentCoversContext(scope AssignmentScope, context AuthSessionContext) bool {
	switch context.Type {
	case "platform":
		return scope.Type == "platform"
	case "tenant":
		return context.TenantID != nil && scope.Type == "tenant" && scope.TenantID != nil && *scope.TenantID == *context.TenantID
	default:
		return false
	}
}
