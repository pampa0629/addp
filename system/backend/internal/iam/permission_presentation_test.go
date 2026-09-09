package iam

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/addp/common/authorization"
)

type systemIAMMessages struct {
	System struct {
		IAM struct {
			Roles struct {
				Actions   map[string]string `json:"actions"`
				Resources map[string]string `json:"resources"`
			} `json:"roles"`
		} `json:"iam"`
	} `json:"system"`
}

func TestTenantCustomizablePermissionsHaveLocalizedPresentationVocabulary(t *testing.T) {
	repositoryRoot := permissionPresentationRepositoryRoot(t)
	report, err := authorization.LoadRepositoryAuthorizationCatalog(repositoryRoot)
	if err != nil {
		t.Fatalf("LoadRepositoryAuthorizationCatalog() error = %v", err)
	}

	resources := map[string]struct{}{}
	actions := map[string]struct{}{}
	for _, permission := range report.Permissions {
		if permission.Status != "active" || !permission.TenantCustomizable {
			continue
		}
		parts := strings.Split(permission.Key, ".")
		if len(parts) < 3 {
			t.Fatalf("tenant-customizable permission key must contain owner, resource, and action: %q", permission.Key)
		}
		resources[strings.Join(parts[1:len(parts)-1], ".")] = struct{}{}
		actions[permission.Action] = struct{}{}
	}

	for _, language := range []string{"zh-cn", "en"} {
		messages := loadSystemIAMMessages(t, repositoryRoot, language)
		assertVocabularyCoverage(t, language, "resource", resources, messages.System.IAM.Roles.Resources)
		assertVocabularyCoverage(t, language, "action", actions, messages.System.IAM.Roles.Actions)
	}
}

func permissionPresentationRepositoryRoot(t *testing.T) string {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve permission presentation test source path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", "..", "..", ".."))
}

func loadSystemIAMMessages(t *testing.T, repositoryRoot, language string) systemIAMMessages {
	t.Helper()
	path := filepath.Join(repositoryRoot, "system", "frontend", "src", "i18n", language+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read System %s messages: %v", language, err)
	}
	var messages systemIAMMessages
	if err := json.Unmarshal(data, &messages); err != nil {
		t.Fatalf("parse System %s messages: %v", language, err)
	}
	return messages
}

func assertVocabularyCoverage(t *testing.T, language, kind string, required map[string]struct{}, localized map[string]string) {
	t.Helper()
	missing := make([]string, 0)
	for key := range required {
		if strings.TrimSpace(localized[key]) == "" {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("System %s IAM %s vocabulary is missing: %s", language, kind, strings.Join(missing, ", "))
	}
}
