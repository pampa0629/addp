package schema

import (
	"errors"
	"fmt"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"io"
	"regexp"
	"strings"

	"gorm.io/gorm"
)

// The embedded trigger files use one header format; the body is an opaque SQL statement.
var mysqlTriggerHeader = regexp.MustCompile(`^CREATE TRIGGER IF NOT EXISTS ([a-z_]+)\n(BEFORE|AFTER) (INSERT|UPDATE|DELETE) ON ([a-z_]+) FOR EACH ROW\n`)

type mysqlTrigger struct {
	Name, Timing, Event, TableName, Body, Definer string
	Position                                      int
}

func CheckMySQLTriggers(conn *gorm.DB, scripts []string) error {
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
	byName := make(map[string]mysqlTrigger)
	for rows.Next() {
		var trigger mysqlTrigger
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
			return fmt.Errorf("invalid embedded MySQL trigger header")
		}
		group := [3]string{header[2], header[3], header[4]}
		positions[group]++
		want := mysqlTrigger{Name: header[1], Timing: header[2], Event: header[3],
			TableName: header[4], Body: strings.TrimSpace(script[len(header[0]):]), Definer: account, Position: positions[group]}
		found, ok := byName[want.Name]
		if !ok {
			missing = append(missing, strings.Replace(strings.TrimSpace(script), "CREATE TRIGGER",
				"CREATE DEFINER="+definer+" TRIGGER", 1))
			continue
		}
		if found != want {
			return fmt.Errorf("installed MySQL trigger %s differs in definition, definer or order", want.Name)
		}
	}
	for i, statement := range missing {
		if err := conn.Exec(statement).Error; err != nil {
			var denied *mysqlDriver.MySQLError
			if errors.As(err, &denied) && denied.Number == 1419 {
				return &PendingMySQLTriggers{Statements: missing[i:]}
			}
			return err
		}
	}
	return nil
}

// PendingMySQLTriggers contains the statements a deployment DBA must apply
// to the same physical database before the installation can publish its revision.
type PendingMySQLTriggers struct{ Statements []string }

func (p *PendingMySQLTriggers) Error() string {
	return fmt.Sprintf("installation requires %d DBA-installed MySQL triggers", len(p.Statements))
}

// WriteSQL emits the pending statements for the deployment DBA.
func (p *PendingMySQLTriggers) WriteSQL(out io.Writer, database string) error {
	if _, err := fmt.Fprintf(out, "USE `%s`;\nDELIMITER $$\n", strings.ReplaceAll(database, "`", "``")); err != nil {
		return err
	}
	for _, stmt := range p.Statements {
		if _, err := fmt.Fprintln(out, stmt+"$$"); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(out, "DELIMITER ;")
	return err
}
