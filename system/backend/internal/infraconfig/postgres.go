package infraconfig

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type postgresConfiguration struct{ dsn string }

var postgresNames = []string{
	"work_mem", "maintenance_work_mem", "log_min_duration_statement", "log_lock_waits",
	"shared_buffers", "max_connections", "TimeZone",
}

const postgresSettings = `SELECT name, setting, coalesce(unit, ''), source, context, pending_restart,
	coalesce(min_val::bigint, 0), coalesce(max_val::bigint, 0),
	has_parameter_privilege(name, 'ALTER SYSTEM') AND has_function_privilege('pg_catalog.pg_reload_conf()', 'EXECUTE')
	FROM pg_settings WHERE name = ANY($1::text[])`

type postgresParameter struct {
	Parameter
	context  string
	min, max int64
}

func readPostgres(ctx context.Context, conn *pgx.Conn, names []string) ([]postgresParameter, error) {
	rows, err := conn.Query(ctx, postgresSettings, names)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer rows.Close()
	result := make([]postgresParameter, 0, len(names))
	for rows.Next() {
		var p postgresParameter
		var canApply bool
		if err := rows.Scan(&p.Name, &p.Value, &p.Unit, &p.Source, &p.context, &p.PendingRestart, &p.min, &p.max, &canApply); err != nil {
			return nil, ErrUnavailable
		}
		switch p.context {
		case "postmaster":
			p.Activation = "restart"
		case "backend", "superuser-backend":
			p.Activation = "new_connection"
		default:
			p.Activation = "reload"
		}
		switch p.Name {
		case "work_mem", "maintenance_work_mem", "log_min_duration_statement", "log_lock_waits":
			p.Editable = canApply && p.Activation == "reload" && (p.Source == "default" || p.Source == "configuration file")
		}
		result = append(result, p)
	}
	if rows.Err() != nil || len(result) != len(names) {
		return nil, ErrUnavailable
	}
	return result, nil
}

func (p postgresConfiguration) read(ctx context.Context) ([]Parameter, error) {
	conn, err := pgx.Connect(ctx, p.dsn)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer conn.Close(context.Background())
	settings, err := readPostgres(ctx, conn, postgresNames)
	if err != nil {
		return nil, err
	}
	parameters := make([]Parameter, 0, len(settings))
	for _, setting := range settings {
		parameters = append(parameters, setting.Parameter)
	}
	return parameters, nil
}

func (p postgresConfiguration) apply(ctx context.Context, name, value string, record func(string) error) (Outcome, error) {
	var statement string
	switch name {
	case "work_mem", "maintenance_work_mem", "log_min_duration_statement", "log_lock_waits":
		statement = "ALTER SYSTEM SET " + name + " = "
	case "shared_buffers", "max_connections", "TimeZone":
		return Outcome{}, ErrReadOnly
	default:
		return Outcome{}, ErrNotFound
	}
	conn, err := pgx.Connect(ctx, p.dsn)
	if err != nil {
		return Outcome{}, ErrUnavailable
	}
	defer conn.Close(context.Background())
	names := []string{name}
	settings, err := readPostgres(ctx, conn, names)
	if err != nil {
		return Outcome{}, err
	}
	before := settings[0]
	if !before.Editable {
		return Outcome{}, ErrReadOnly
	}
	if name == "log_lock_waits" {
		if value != "on" && value != "off" {
			return Outcome{}, ErrInvalid
		}
	} else if value, err = intValue(value, before.min, before.max); err != nil {
		return Outcome{}, err
	}
	if err := record(before.Value); err != nil {
		return Outcome{}, err
	}

	out := Outcome{PreviousValue: before.Value, RequestedValue: value, Application: "unconfirmed", Persistence: "unconfirmed"}
	// The name comes from the allowlist. The value is a normalized integer or on/off.
	if _, err := conn.Exec(ctx, statement+"'"+value+"'"); err != nil {
		out.FailureStage = "write"
		return out, nil
	}
	out.Persistence = "confirmed"
	var reload bool
	if err := conn.QueryRow(ctx, "SELECT pg_reload_conf()").Scan(&reload); err != nil || !reload {
		out.FailureStage = "reload"
		return out, nil
	}
	// The signal is asynchronous. Observe the applied default rather than returning the signal result.
	verify, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		current, readErr := readPostgres(verify, conn, names)
		if readErr == nil {
			out.Current = &current[0].Parameter
			if out.Current.Value == value && out.Current.Source == "configuration file" && !out.Current.PendingRestart {
				out.Application = "confirmed"
				return out, nil
			}
		}
		select {
		case <-verify.Done():
			out.FailureStage = "verify"
			return out, nil
		case <-ticker.C:
		}
	}
}
