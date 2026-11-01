// Package redissource owns deployment-side Redis observation, not Monitor Web
// requests. It reads fixed instance statistics without accessing business keys.
package redissource

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const MaxInfoBytes = 256 * 1024

var (
	ErrInvalidSnapshot = errors.New("Redis observation snapshot rejected")
	ErrInvalidAccount  = errors.New("Redis observation account declaration rejected")
	ErrUnavailable     = errors.New("Redis observation source unavailable")
	runIDPattern       = regexp.MustCompile(`^[0-9a-f]{40}$`)
	hashPattern        = regexp.MustCompile(`^[0-9a-f]{64}$`)
	versionPattern     = regexp.MustCompile(`^7\.[0-9]+\.[0-9]+$`)
)

// Snapshot contains only fixed numeric statistics and private runtime identity.
// Neither Snapshot nor RunID is a browser DTO or an arbitrary metric-label map.
type Snapshot struct {
	RunID     string
	SampledAt time.Time
	Values    map[string]uint64
}

// ObserverACL returns the complete ACL SETUSER arguments owned by deployment.
// It does not connect, install, adopt an existing account or persist credentials.
// A disabled account loses command/key/channel/password permissions even on
// already authenticated connections; merely setting off would not ensure this.
func ObserverACL(scope, passwordSHA256 string, enabled bool) ([]string, error) {
	if scope != "infra" && scope != "business" {
		return nil, ErrInvalidAccount
	}
	args := []string{"ACL", "SETUSER", "addp_" + scope + "_observer", "reset"}
	if !enabled {
		if passwordSHA256 != "" {
			return nil, ErrInvalidAccount
		}
		return append(args, "off"), nil
	}
	if !hashPattern.MatchString(passwordSHA256) {
		return nil, ErrInvalidAccount
	}
	return append(args, "on", "#"+passwordSHA256, "+hello", "+ping", "+info"), nil
}

var requiredFields = []struct{ section, field string }{
	{"Server", "uptime_in_seconds"},
	{"Clients", "connected_clients"}, {"Clients", "maxclients"}, {"Clients", "blocked_clients"},
	{"Memory", "used_memory"}, {"Memory", "used_memory_rss"}, {"Memory", "maxmemory"}, {"Memory", "mem_not_counted_for_evict"},
	{"Stats", "total_commands_processed"}, {"Stats", "keyspace_hits"}, {"Stats", "keyspace_misses"},
	{"Stats", "evicted_keys"}, {"Stats", "rejected_connections"},
}

// parseFields deliberately discards unselected fields, rather than forwarding
// raw INFO, paths, endpoint strings or free-form error labels to consumers.
func parseFields(info string) (map[string]map[string]string, error) {
	if len(info) == 0 || len(info) > MaxInfoBytes || strings.ContainsRune(info, '\x00') {
		return nil, ErrInvalidSnapshot
	}
	sections := make(map[string]map[string]string)
	var fields map[string]string
	for _, line := range strings.Split(info, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			continue
		}
		if len(line) > 4096 {
			return nil, ErrInvalidSnapshot
		}
		if strings.HasPrefix(line, "# ") {
			section := strings.TrimPrefix(line, "# ")
			if section == "" || sections[section] != nil {
				return nil, ErrInvalidSnapshot
			}
			fields = make(map[string]string)
			sections[section] = fields
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if fields == nil || !ok || key == "" {
			return nil, ErrInvalidSnapshot
		}
		if _, exists := fields[key]; exists {
			return nil, ErrInvalidSnapshot
		}
		fields[key] = value
	}
	return sections, nil
}

func unsigned(value string) (uint64, error) {
	if value == "" || len(value) > 20 || (len(value) > 1 && value[0] == '0') {
		return 0, ErrInvalidSnapshot
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return 0, ErrInvalidSnapshot
		}
	}
	number, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, ErrInvalidSnapshot
	}
	return number, nil
}

func serverIdentity(sections map[string]map[string]string) (string, uint64, error) {
	server := sections["Server"]
	if !runIDPattern.MatchString(server["run_id"]) || server["redis_mode"] != "standalone" || !versionPattern.MatchString(server["redis_version"]) {
		return "", 0, ErrInvalidSnapshot
	}
	uptime, err := unsigned(server["uptime_in_seconds"])
	if err != nil {
		return "", 0, err
	}
	return server["run_id"], uptime, nil
}

func Parse(info string, sampledAt time.Time) (Snapshot, error) {
	sections, err := parseFields(info)
	if err != nil || sampledAt.IsZero() {
		return Snapshot{}, ErrInvalidSnapshot
	}
	runID, _, err := serverIdentity(sections)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot := Snapshot{RunID: runID, SampledAt: sampledAt.UTC(), Values: make(map[string]uint64)}
	for _, selected := range requiredFields {
		number, err := unsigned(sections[selected.section][selected.field])
		if err != nil {
			return Snapshot{}, err
		}
		snapshot.Values[selected.field] = number
	}
	if snapshot.Values["maxclients"] == 0 || snapshot.Values["mem_not_counted_for_evict"] > snapshot.Values["used_memory"] {
		return Snapshot{}, ErrInvalidSnapshot
	}
	snapshot.Values["memory_counted_for_limit_bytes"] = snapshot.Values["used_memory"] - snapshot.Values["mem_not_counted_for_evict"]
	persistence := sections["Persistence"]
	for _, field := range []string{"loading", "aof_enabled"} {
		number, err := unsigned(persistence[field])
		if err != nil || number > 1 {
			return Snapshot{}, ErrInvalidSnapshot
		}
		snapshot.Values[field] = number
	}
	statusFields := []string{"rdb_last_bgsave_status"}
	if snapshot.Values["aof_enabled"] == 1 {
		statusFields = append(statusFields, "aof_last_write_status", "aof_last_bgrewrite_status")
	}
	for _, field := range statusFields {
		switch persistence[field] {
		case "ok":
			snapshot.Values[field] = 1
		case "err":
			snapshot.Values[field] = 0
		default:
			return Snapshot{}, ErrInvalidSnapshot
		}
	}
	// A successful Errorstats section without OOM means zero, not a missing
	// response. Never create series labelled with arbitrary Redis error names.
	errorstats, ok := sections["Errorstats"]
	if !ok {
		return Snapshot{}, ErrInvalidSnapshot
	}
	if _, disabled := errorstats["errorstat_ERRORSTATS_DISABLED"]; disabled {
		return Snapshot{}, ErrInvalidSnapshot
	}
	oom := uint64(0)
	if value, ok := errorstats["errorstat_OOM"]; ok {
		if !strings.HasPrefix(value, "count=") {
			return Snapshot{}, ErrInvalidSnapshot
		}
		oom, err = unsigned(strings.TrimPrefix(value, "count="))
		if err != nil {
			return Snapshot{}, err
		}
	}
	snapshot.Values["oom_errors_total"] = oom
	return snapshot, nil
}

// VerifyBoundary prevents a snapshot crossing an in-container Redis restart.
// It does not establish Docker/container/host identity or detect every reset.
func (s Snapshot) VerifyBoundary(serverInfo string) error {
	sections, err := parseFields(serverInfo)
	if err != nil {
		return err
	}
	runID, uptime, err := serverIdentity(sections)
	if err != nil || runID != s.RunID || uptime < s.Values["uptime_in_seconds"] {
		return ErrInvalidSnapshot
	}
	return nil
}
