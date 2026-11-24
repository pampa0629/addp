package cleanup

import (
	"fmt"

	"github.com/addp/common/events"
	"gorm.io/gorm"
)

// MySQLResultUpdateGuard is included in the owner's deployment trigger batch.
const MySQLResultUpdateGuard = `CREATE TRIGGER IF NOT EXISTS cleanup_result_immutable
BEFORE UPDATE ON cleanup_results FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='cleanup result is immutable'`

// PrepareMySQLResultTable prepares the shared result shape in its owner's database.
func PrepareMySQLResultTable(db *gorm.DB, owner string) error {
	if !events.CleanupModuleValid(owner) {
		return fmt.Errorf("invalid cleanup Owner %q", owner)
	}
	var selected string
	if err := db.Raw("SELECT DATABASE()").Row().Scan(&selected); err != nil {
		return err
	}
	if selected != owner {
		return fmt.Errorf("cleanup result owner %q differs from selected database %q", owner, selected)
	}
	return db.Exec(`CREATE TABLE IF NOT EXISTS cleanup_results (
    execution_id VARCHAR(36) NOT NULL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    attempt INT NOT NULL,
    request_digest VARCHAR(64) NOT NULL,
    payload LONGTEXT NOT NULL,
    created_at DATETIME(6) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin`).Error
}
