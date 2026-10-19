package iam

import "testing"

func TestAdministratorRecoveryTargetsBindPrincipalsAndRetainSuspension(t *testing.T) {
	targets := []RecoveryAdministratorTarget{}
	for i, role := range bootstrapRoleOrder {
		targets = append(targets, RecoveryAdministratorTarget{RoleKey: role, PrincipalID: int64(i + 1), PrincipalStatus: PrincipalStatusActive, AccountStatus: LocalAccountStatusActive})
	}
	targets[0].PrincipalStatus = PrincipalStatusSuspended
	targets = append(targets, RecoveryAdministratorTarget{RoleKey: bootstrapRoleOrder[0], PrincipalID: 4, PrincipalStatus: PrincipalStatusActive, AccountStatus: LocalAccountStatusActive})
	if err := validateRecoveryTargets(targets); err != nil {
		t.Fatalf("multiple distinct holders or suspended identity rejected: %v", err)
	}
	duplicate := append([]RecoveryAdministratorTarget(nil), targets...)
	duplicate[3].PrincipalID = targets[1].PrincipalID
	if err := validateRecoveryTargets(duplicate); err == nil {
		t.Fatal("same identity occupied multiple recovery slots")
	}
	unavailable := append([]RecoveryAdministratorTarget(nil), targets...)
	unavailable[0].PrincipalStatus = PrincipalStatusDeactivated
	if err := validateRecoveryTargets(unavailable); err == nil {
		t.Fatal("credential recovery accepted a deactivated principal")
	}
}
