package main

import (
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestRecoveryCommandsRequireExplicitNonSecretCorrelationIDs(t *testing.T) {
	id := uuid.NewString()
	for _, args := range [][]string{{"prepare"}, {"apply", "7"}, {"status", "7", id}} {
		if _, _, _, err := parseRecoveryCommand(args); err != nil {
			t.Fatalf("valid command %s rejected", args[0])
		}
	}
	for _, args := range [][]string{nil, {"apply"}, {"apply", "0"}, {"apply", "07"}, {"apply", "7", "secret"}, {"status", "7"}, {"status", "7", uuid.Nil.String()}, {"status", "7", "addp_ir_do-not-echo"}} {
		_, _, _, err := parseRecoveryCommand(args)
		if err == nil || strings.Contains(err.Error(), "do-not-echo") {
			t.Fatal("invalid command accepted or echoed secret material")
		}
	}
	// Rejection precedes configuration loading, database connection and MFA setup.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	for _, args := range [][]string{{"prepare"}, {"apply", "7"}, {"status", "7", id}} {
		if err := run(args, r, w); err == nil || !strings.Contains(err.Error(), "必须直接运行在交互终端") {
			t.Fatal("nonterminal command reached database setup")
		}
	}
}

func TestRecoveryOutcomeGuidanceIsLocalized(t *testing.T) {
	t.Setenv("LC_ALL", "en_US.UTF-8")
	if !strings.Contains(recoveryText("state_unknown"), "Outcome unknown") {
		t.Fatal("English outcome guidance missing")
	}
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	if !strings.Contains(recoveryText("state_unknown"), "结果未知") {
		t.Fatal("Chinese outcome guidance missing")
	}
}
