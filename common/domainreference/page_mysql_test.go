package domainreference

import (
	"os"
	"testing"

	"github.com/addp/common/infra/database"
	"github.com/addp/common/infra/database/mysql"
	"gorm.io/gorm"
)

func TestReferencePageAgainstMySQL(t *testing.T) {
	name := os.Getenv("ADDP_TEST_MYSQL_PLATFORM_DATABASE")
	if name == "" {
		t.Skip("run the platform-database MySQL gate")
	}
	driver, err := mysql.Dialector(database.Config{
		Host: os.Getenv("ADDP_TEST_MYSQL_HOST"), Port: os.Getenv("ADDP_TEST_MYSQL_PORT"),
		User: os.Getenv("ADDP_TEST_MYSQL_USER"), Password: os.Getenv("ADDP_TEST_MYSQL_PASSWORD"), DBName: name,
	})
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(driver, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	// Every evidence row occurs twice. The first identity has 101 distinct
	// positions, the second only one. Both identities exceed JS integer precision.
	query := db.Raw(`WITH RECURSIVE positions(n) AS (
		SELECT 1 UNION ALL SELECT n+1 FROM positions WHERE n<102
	) SELECT 'element' AS source_type,
		CASE WHEN n<=101 THEN '9007199254740993' ELSE '9007199254740995' END AS source_identity,
		'model' AS owner_module, 'entity' AS resource_type, '43' AS resource_id, '实体名' AS resource_name,
		LPAD(n,3,'0') AS position, '17' AS revision_id, NULL AS resource_revision_id, 'approved' AS state,
		'model.entity.element' AS relation_kind FROM positions CROSS JOIN (SELECT 1 UNION ALL SELECT 2) copies`)
	first, err := ReadPage(t.Context(), db, query, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.Total != 2 || first.TotalPages != 2 || len(first.Data) != 1 {
		t.Fatalf("unexpected first page: %+v", first)
	}
	ref := first.Data[0]
	if ref.SourceIdentity != "9007199254740993" || !ref.EvidenceTruncated || len(ref.Evidence) != 100 ||
		ref.Evidence[0].Position != "001" || ref.Evidence[99].Position != "100" ||
		ref.Evidence[0].ResourceName != "实体名" || ref.Evidence[0].RevisionID != "17" {
		t.Fatal("first page lost distinct, ordered or bounded evidence")
	}
	second, err := ReadPage(t.Context(), db, query, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Data) != 1 || second.Data[0].SourceIdentity != "9007199254740995" ||
		second.Data[0].EvidenceTruncated || len(second.Data[0].Evidence) != 1 || second.Data[0].Evidence[0].Position != "102" {
		t.Fatalf("unexpected second page: %+v", second)
	}
	empty, err := ReadPage(t.Context(), db, query, 3, 1)
	if err != nil || empty.Total != 2 || len(empty.Data) != 0 {
		t.Fatalf("empty page = %+v, error = %v", empty, err)
	}
}
