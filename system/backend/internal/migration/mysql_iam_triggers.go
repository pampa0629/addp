package migration

import (
	"fmt"
	"regexp"
	"strings"

	"gorm.io/gorm"
)

// The embedded trigger files use one header format; the body is an opaque SQL statement.
var mysqlTriggerHeader = regexp.MustCompile(`^CREATE TRIGGER IF NOT EXISTS ([a-z_]+)\n(BEFORE|AFTER) (INSERT|UPDATE|DELETE) ON ([a-z_]+) FOR EACH ROW\n`)

type mysqlIAMTrigger struct {
	Name, Timing, Event, TableName, Body, Definer string
	Position                                      int
}

func checkMySQLIAMTriggers(conn *gorm.DB, scripts []string) error {
	var account string
	if err := conn.Raw("SELECT CURRENT_USER()").Row().Scan(&account); err != nil {
		return err
	}
	at := strings.LastIndexByte(account, '@')
	if at <= 0 || strings.ContainsAny(account, "\x00\r\n") {
		return fmt.Errorf("invalid MySQL installation account")
	}
	quote := func(s string) string { return "`" + strings.ReplaceAll(s, "`", "``") + "`" }
	definer := quote(account[:at]) + "@" + quote(account[at+1:])
	rows, err := conn.Raw(`SELECT trigger_name, action_timing, CAST(event_manipulation AS CHAR), event_object_table, action_statement, definer, action_order
 FROM information_schema.triggers WHERE trigger_schema=DATABASE()`).Rows()
	if err != nil {
		return err
	}
	defer rows.Close()
	byName := make(map[string]mysqlIAMTrigger)
	for rows.Next() {
		var trigger mysqlIAMTrigger
		if err := rows.Scan(&trigger.Name, &trigger.Timing, &trigger.Event, &trigger.TableName, &trigger.Body, &trigger.Definer, &trigger.Position); err != nil {
			return err
		}
		byName[trigger.Name] = trigger
	}
	if err := rows.Err(); err != nil {
		return err
	}
	var missing []string
	positions := make(map[[3]string]int)
	for _, script := range scripts {
		header := mysqlTriggerHeader.FindStringSubmatch(script)
		if header == nil {
			return fmt.Errorf("invalid embedded System IAM trigger header")
		}
		group := [3]string{header[2], header[3], header[4]}
		positions[group]++
		want := mysqlIAMTrigger{Name: header[1], Timing: header[2], Event: header[3],
			TableName: header[4], Body: strings.TrimSpace(script[len(header[0]):]), Definer: account, Position: positions[group]}
		found, ok := byName[want.Name]
		if !ok {
			missing = append(missing, strings.Replace(strings.TrimSpace(script), "CREATE TRIGGER",
				"CREATE DEFINER="+definer+" TRIGGER", 1))
			continue
		}
		if found != want {
			return fmt.Errorf("installed System IAM trigger %s differs in definition, definer or order", want.Name)
		}
	}
	if len(missing) > 0 {
		return &PendingMySQLIAMTriggers{Statements: missing}
	}
	return nil
}

// PendingMySQLIAMTriggers contains the statements a deployment DBA must apply
// to the same physical database before the installation can publish its revision.
type PendingMySQLIAMTriggers struct{ Statements []string }

func (p *PendingMySQLIAMTriggers) Error() string {
	return fmt.Sprintf("System IAM requires %d DBA-installed MySQL triggers", len(p.Statements))
}
