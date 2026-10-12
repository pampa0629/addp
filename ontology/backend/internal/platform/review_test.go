package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTransferReviewSourceDrift(t *testing.T) {
	snapshot, err := CompileTransferRelease()
	if err != nil {
		t.Fatal(err)
	}
	review, _ := snapshot.Review()
	root := filepath.Clean("../../../..")
	for _, source := range review.Sources {
		t.Run(source.ID, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(source.Path)))
			if err != nil {
				t.Fatal(err)
			}
			if err := checkSource(data, source); err != nil {
				t.Fatalf("%v; review semantics and update provenance/revision, do not regenerate blindly", err)
			}
			if err := checkSourceAtRevision(root, source); err != nil {
				t.Fatalf("%v; commit reviewed sources before freezing provenance", err)
			}
		})
	}
}

// Git proves publication provenance only in the repository gate, not at runtime.
func checkSourceAtRevision(root string, source Source) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, "git", "-C", root, "show", source.Commit+":"+source.Path).Output()
	if err != nil {
		return fmt.Errorf("unavailable committed source: %s at %s: %w", source.Path, source.Commit, err)
	}
	if err := checkSource(data, source); err != nil {
		return fmt.Errorf("committed source at %s: %w", source.Commit, err)
	}
	return nil
}

func TestSourceRevisionRejectsUnfrozenProvenance(t *testing.T) {
	// Bare objects avoid hooks, user worktrees and incidental file ownership.
	root := t.TempDir()
	git := func(input string, args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root, "-c", "commit.gpgsign=false", "-c", "user.name=Ontology test", "-c", "user.email=ontology-test@example.invalid"}, args...)...)
		cmd.Stdin = strings.NewReader(input)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("fixture git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("", "init", "--bare", "--quiet")
	data := []byte("reviewed function anchor")
	blob := git(string(data), "hash-object", "-w", "--stdin")
	tree := git("100644 blob "+blob+"\tfile.go\n", "mktree")
	commit := git("reviewed source\n", "commit-tree", tree)
	hash := sha256.Sum256(data)
	source := Source{Path: "file.go", Commit: commit, Anchor: "function anchor", SHA256: hex.EncodeToString(hash[:])}
	if err := checkSourceAtRevision(root, source); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Source)
	}{
		{"missing_commit", func(s *Source) { s.Commit = strings.Repeat("0", 40) }},
		{"missing_file", func(s *Source) { s.Path = "missing.go" }},
		{"old_commit_new_hash", func(s *Source) {
			changed := sha256.Sum256(append(bytes.Clone(data), 'x'))
			s.SHA256 = hex.EncodeToString(changed[:])
		}},
		{"missing_anchor", func(s *Source) { s.Anchor = "deleted function" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			invalid := source
			tc.mutate(&invalid)
			if err := checkSourceAtRevision(root, invalid); err == nil {
				t.Fatal("unfrozen provenance accepted")
			}
		})
	}
}

func checkSource(data []byte, source Source) error {
	hash := sha256.Sum256(data)
	if hex.EncodeToString(hash[:]) != source.SHA256 {
		return fmt.Errorf("source drift: %s", source.Path)
	}
	if !bytes.Contains(data, []byte(source.Anchor)) {
		return fmt.Errorf("missing source anchor: %s in %s", source.Anchor, source.Path)
	}
	return nil
}

func TestSourceDriftCheckRejectsMutation(t *testing.T) {
	data := []byte("reviewed function anchor")
	hash := sha256.Sum256(data)
	source := Source{Path: "owner/file.go", Anchor: "function anchor", SHA256: hex.EncodeToString(hash[:])}
	if err := checkSource(data, source); err != nil {
		t.Fatal(err)
	}
	if err := checkSource(append(bytes.Clone(data), 'x'), source); err == nil {
		t.Fatal("changed source content accepted")
	}
	source.Anchor = "deleted anchor"
	if err := checkSource(data, source); err == nil {
		t.Fatal("deleted source anchor accepted")
	}
}

func TestTransferConfigCheckIsRequiredButNotAnExecutionGuarantee(t *testing.T) {
	snapshot, err := CompileTransferRelease()
	if err != nil {
		t.Fatal(err)
	}
	review, _ := snapshot.Review()
	found := false
	for _, coverage := range review.Coverage {
		if coverage.ID == "transfer.task.create" {
			found = coverage.State == "modeled" && strings.Contains(coverage.Summary["en"], "unknown") && strings.Contains(coverage.Summary["en"], "execution")
		}
		if coverage.ID == "transfer.task.config.check" {
			t.Fatal("precheck is a creation condition, not a separate modeled capability")
		}
	}
	if !found {
		t.Fatal("missing precheck coverage and execution limitation")
	}
	context, _ := snapshot.Context()
	found = false
	for _, requirement := range context.Requirements {
		for _, tool := range requirement.Tools {
			if tool == "transfer.task.config.check" {
				found = requirement.ID == "config_precheck"
			}
		}
	}
	if !found {
		t.Fatal("missing formal precheck tool requirement")
	}
	found = false
	for _, condition := range context.Operation.InputsRequired {
		found = found || condition == "readonly_config_check"
	}
	if !found {
		t.Fatal("missing readonly config check creation condition")
	}
}

func TestReviewBoundToSnapshotAndNotAgentContext(t *testing.T) {
	s, err := CompileTransferRelease()
	if err != nil {
		t.Fatal(err)
	}
	r, _ := s.Review()
	d, _ := s.Context()
	contextBytes, _ := json.Marshal(d)
	if bytes.Contains(contextBytes, []byte("sources")) || bytes.Contains(contextBytes, []byte("coverage")) {
		t.Fatal("review inflated the Agent context")
	}
	if len(r.Bindings) != len(ReviewSubjects(d)) || r.Coverage[0].State != "modeled" {
		t.Fatal("incomplete member evidence")
	}
	r.Sources[0].Use["en"] = "changed"
	r.Bindings[0].Sources[0] = "changed"
	r.Coverage[0].Summary["en"] = "changed"
	fresh, _ := s.Review()
	if fresh.Sources[0].Use["en"] == "changed" || fresh.Bindings[0].Sources[0] == "changed" || fresh.Coverage[0].Summary["en"] == "changed" {
		t.Fatal("review mutation escaped immutable snapshot")
	}
	fresh.Sources[0].SHA256 = strings.Repeat("a", 64)
	data, _ := json.Marshal(fresh)
	other, err := Compile(transferDefinition, data)
	if err != nil || other.Digest() == s.Digest() {
		t.Fatal("changed provenance did not change release digest", err)
	}
	var old map[string]json.RawMessage
	_ = json.Unmarshal(s.CanonicalJSON(), &old)
	delete(old, "review")
	old["contract"] = json.RawMessage(`"addp.platform-definition/v1"`)
	old["compiler"] = json.RawMessage(`"addp.platform-compiler/v1"`)
	legacy, _ := json.Marshal(old)
	hash := sha256.Sum256(legacy)
	if _, err := Restore(legacy, hex.EncodeToString(hash[:])); err == nil {
		t.Fatal("old snapshot was implicitly upgraded")
	}
}

func TestCompileRejectsInvalidReview(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Review)
	}{
		{"method", func(r *Review) { r.Method = "automatic" }},
		{"scope", func(r *Review) { r.Scope = "all_transfer" }},
		{"missing_source", func(r *Review) { r.Sources = r.Sources[1:] }},
		{"duplicate_source", func(r *Review) { r.Sources = append(r.Sources, r.Sources[0]) }},
		{"path_traversal", func(r *Review) { r.Sources[0].Path = "../private" }},
		{"absolute_path", func(r *Review) { r.Sources[0].Path = "/private" }},
		{"invalid_hash", func(r *Review) { r.Sources[0].SHA256 = "latest" }},
		{"invalid_commit", func(r *Review) { r.Sources[0].Commit = "main" }},
		{"invalid_kind", func(r *Review) { r.Sources[0].Kind = "live_data" }},
		{"invalid_owner", func(r *Review) { r.Sources[0].Owner = "common_python" }},
		{"missing_kind", func(r *Review) {
			for i := range r.Sources {
				if r.Sources[i].Kind == "normative" {
					r.Sources[i].Kind = "procedure"
				}
			}
		}},
		{"missing_binding", func(r *Review) { r.Bindings = r.Bindings[1:] }},
		{"duplicate_binding", func(r *Review) { r.Bindings = append(r.Bindings, r.Bindings[0]) }},
		{"unknown_subject", func(r *Review) { r.Bindings[0].Subject = "operation/other" }},
		{"unknown_ref", func(r *Review) { r.Bindings[0].Sources = []string{"missing"} }},
		{"duplicate_ref", func(r *Review) { r.Bindings[0].Sources = []string{"manifest", "manifest"} }},
		{"missing_modeled", func(r *Review) { r.Coverage = r.Coverage[1:] }},
		{"conflicting_coverage", func(r *Review) { r.Coverage[0].State = "not_modeled" }},
		{"unbound_coverage", func(r *Review) { r.Coverage[0].ID = "another.operation" }},
		{"invalid_state", func(r *Review) { r.Coverage[0].State = "complete" }},
		{"duplicate_coverage", func(r *Review) { r.Coverage = append(r.Coverage, r.Coverage[0]) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var r Review
			_ = json.Unmarshal(transferReview, &r)
			tc.mutate(&r)
			data, _ := json.Marshal(r)
			if _, err := Compile(transferDefinition, data); err == nil {
				t.Fatal("invalid evidence accepted")
			}
		})
	}
	for _, data := range [][]byte{nil, []byte("null"), []byte(`{}`), bytes.Replace(transferReview, []byte(`"method":`), []byte(`"method":"curated","method":`), 1)} {
		if _, err := Compile(transferDefinition, data); err == nil {
			t.Fatal("non-strict evidence accepted")
		}
	}
}
