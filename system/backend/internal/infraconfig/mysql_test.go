package infraconfig

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/system/internal/config"
	"gorm.io/gorm"
)

func TestMySQLReadOnlyInstanceConfiguration(t *testing.T) {
	expected, ok := os.LookupEnv("ADDP_TEST_MYSQL_GLOBAL_SETTINGS")
	if !ok {
		t.Skip("requires the disposable System MySQL installation gate")
	}
	db, err := bootstrap.OpenPlatformRuntime(config.LoadPlatformDatabase(), "system", &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	service := New(&config.Config{InfraDatabaseProvider: "mysql"}, db)
	components := service.Read(t.Context())
	instance := components[0]
	if instance.Provider != "mysql" || instance.Status != "available" || instance.ObservedAt == nil || len(instance.Parameters) != 3 {
		t.Fatalf("instance observation: %+v", instance)
	}
	var observed []string
	for _, p := range instance.Parameters {
		if p.Editable {
			t.Fatalf("unexpected editable parameter: %s", p.Name)
		}
		observed = append(observed, p.Name+"="+p.Value)
		_, err := service.Apply(t.Context(), "database", p.Name, p.Value, func(string, string) error { t.Fatal("read-only application called the write audit"); return nil })
		if !errors.Is(err, ErrReadOnly) {
			t.Fatalf("expected read-only refusal for %s: %v", p.Name, err)
		}
	}
	if strings.Join(observed, "\t") != expected {
		t.Fatalf("observations do not match instance globals: %v", observed)
	}
	_, err = service.Apply(t.Context(), "database", "unsupported_setting", "x", nil)
	if !errors.Is(err, ErrNotFound) {
		t.Fatal("unexpected outcome for unknown parameter", err)
	}
}
