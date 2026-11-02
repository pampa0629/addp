package neo4j

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// These observations qualify a candidate; they are not production recovery evidence.
type nativeIdentitySnapshot struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	CreationDate string `json:"creation_date"`
	Version      string `json:"version"`
	Edition      string `json:"edition"`
	SystemID     string `json:"system_id"`
	DatabaseID   string `json:"database_id"`
	Original     int64  `json:"original"`
	PostDump     int64  `json:"post_dump"`
}

func (s nativeIdentitySnapshot) sameCandidate(other nativeIdentitySnapshot) bool {
	return s.ID == other.ID && s.Name == other.Name && s.CreationDate == other.CreationDate &&
		s.Version == other.Version && s.Edition == other.Edition
}

func identityRecord(t *testing.T, ctx context.Context, driver neo4jdriver.DriverWithContext, purpose, query string) *neo4jdriver.Record {
	t.Helper()
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{DatabaseName: "neo4j", AccessMode: neo4jdriver.AccessModeRead})
	defer session.Close(ctx) //nolint:errcheck
	result, err := session.Run(ctx, query, nil)
	if err != nil {
		var native *neo4jdriver.Neo4jError
		if errors.As(err, &native) {
			t.Fatalf("native identity read failed purpose=%s code=%s", purpose, native.Code)
		}
		t.Fatalf("native identity read failed purpose=%s type=%T", purpose, err)
	}
	rows, err := result.Collect(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatalf("native identity read requires exactly one row: purpose=%s rows=%d error=%T", purpose, len(rows), err)
	}
	return rows[0]
}

func observeIdentity(t *testing.T, ctx context.Context, driver neo4jdriver.DriverWithContext) nativeIdentitySnapshot {
	t.Helper()
	info := identityRecord(t, ctx, driver, "database_info", "CALL db.info() YIELD id, name, creationDate RETURN id, name, creationDate")
	components := identityRecord(t, ctx, driver, "components", "CALL dbms.components() YIELD name, versions, edition RETURN name, versions, edition")
	system := identityRecord(t, ctx, driver, "instance_info", "CALL dbms.info() YIELD id RETURN id")
	database := identityRecord(t, ctx, driver, "database_catalog", "SHOW DATABASE neo4j YIELD *")
	counts := identityRecord(t, ctx, driver, "fixture_counts", "MATCH (n:ADDPIdentityT2) RETURN count(CASE WHEN n.phase = 'original' THEN 1 END) AS original_count, count(CASE WHEN n.phase = 'post_dump' THEN 1 END) AS post_dump_count")
	text := func(row *neo4jdriver.Record, key string) string {
		value, ok := row.Get(key)
		parsed, valid := value.(string)
		if !ok || !valid || parsed == "" {
			t.Fatalf("missing or invalid native identity field: %s", key)
		}
		return parsed
	}
	versions, _ := components.Get("versions")
	list, ok := versions.([]any)
	if !ok || len(list) != 1 || list[0] != "5.26.24" || text(components, "name") != "Neo4j Kernel" {
		t.Fatal("identity candidate requires the fixed Neo4j 5.26.24 product")
	}
	count := func(key string) int64 {
		value, _ := counts.Get(key)
		number, ok := value.(int64)
		if !ok || number < 0 {
			t.Fatalf("invalid native fixture count: %s", key)
		}
		return number
	}
	snapshot := nativeIdentitySnapshot{ID: text(info, "id"), Name: text(info, "name"),
		CreationDate: text(info, "creationDate"), Version: list[0].(string), Edition: text(components, "edition"),
		SystemID: text(system, "id"), Original: count("original_count"), PostDump: count("post_dump_count")}
	if value, available := database.Get("databaseID"); available {
		var valid bool
		snapshot.DatabaseID, valid = value.(string)
		if !valid || snapshot.DatabaseID == "" {
			t.Fatal("invalid native databaseID observation")
		}
	}
	hexIdentity := regexp.MustCompile(`(?i)^[0-9a-f]{64}$`)
	if !hexIdentity.MatchString(snapshot.ID) || !hexIdentity.MatchString(snapshot.SystemID) || snapshot.Name != "neo4j" || snapshot.Edition != "community" {
		t.Fatal("invalid native target identity or edition")
	}
	if _, err := time.Parse(time.RFC3339Nano, snapshot.CreationDate); err != nil {
		t.Fatal("invalid native creation date")
	}
	return snapshot
}

func seedIdentityFixture(t *testing.T, ctx context.Context, driver neo4jdriver.DriverWithContext, phase string) {
	t.Helper()
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{DatabaseName: "neo4j", AccessMode: neo4jdriver.AccessModeWrite})
	defer session.Close(ctx) //nolint:errcheck
	result, err := session.Run(ctx, "CREATE (:ADDPIdentityT2 {phase: $phase})", map[string]any{"phase": phase})
	if err != nil {
		t.Fatalf("owned identity fixture seed failed: %T", err)
	}
	summary, err := result.Consume(ctx)
	if err != nil || summary.Counters().NodesCreated() != 1 {
		t.Fatalf("owned identity fixture seed not confirmed: %T", err)
	}
}

func saveIdentitySnapshot(t *testing.T, directory, name string, value nativeIdentitySnapshot) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(directory, name+".json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal("cannot create private native identity observation")
	}
	defer f.Close() //nolint:errcheck
	if json.NewEncoder(f).Encode(value) != nil {
		t.Fatal("cannot save native identity observation")
	}
}

func loadIdentitySnapshot(t *testing.T, directory, name string) nativeIdentitySnapshot {
	t.Helper()
	path := filepath.Join(directory, name+".json")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() > 4096 {
		t.Fatal("invalid private native identity observation")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal("cannot read private native identity observation")
	}
	defer f.Close() //nolint:errcheck
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	var snapshot nativeIdentitySnapshot
	if decoder.Decode(&snapshot) != nil || decoder.Decode(new(any)) != io.EOF {
		t.Fatal("invalid native identity observation JSON")
	}
	return snapshot
}

// The standard gate controls the exact owned container/volume lifecycle between calls.
func TestNativeNeo4jTargetIdentityLifecycle(t *testing.T) {
	if os.Getenv("ADDP_NEO4J_INTEGRATION") != "1" {
		t.Skip("owned Neo4j T2 gate required")
	}
	phase, directory := os.Getenv("NEO4J_T2_IDENTITY_PHASE"), os.Getenv("NEO4J_T2_IDENTITY_DIR")
	dir, err := os.Lstat(directory)
	if err != nil || !dir.IsDir() || dir.Mode()&os.ModeSymlink != 0 || dir.Mode().Perm() != 0o700 {
		t.Fatal("owned private identity directory required")
	}
	host, portText, err := net.SplitHostPort(os.Getenv("NEO4J_T2_ENDPOINT"))
	if err != nil || host != "127.0.0.1" {
		t.Fatal("owned loopback endpoint required")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 || os.Getenv("NEO4J_T2_PASSWORD") == "" {
		t.Fatal("invalid owned Neo4j connection")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	conn := plugin.ConnectionInfo{"host": host, "port": port, "user": "neo4j", "password": os.Getenv("NEO4J_T2_PASSWORD"), "database": "neo4j"}
	provider := &Neo4jPlugin{}
	driver, err := provider.createDriver(ctx, conn)
	if err != nil {
		t.Fatalf("owned native driver failed: %T", err)
	}
	defer driver.Close(ctx) //nolint:errcheck
	current := observeIdentity(t, ctx, driver)
	if phase == "baseline" {
		if current.Original != 0 || current.PostDump != 0 {
			t.Fatal("identity fixture is not fresh")
		}
		seedIdentityFixture(t, ctx, driver, "original")
		current = observeIdentity(t, ctx, driver)
		if current.Original != 1 || current.PostDump != 0 {
			t.Fatal("original identity fixture missing")
		}
		saveIdentitySnapshot(t, directory, "baseline", current)
		conn["password"] = os.Getenv("NEO4J_T2_PASSWORD") + "-invalid"
		wrong, err := provider.createDriver(ctx, conn)
		if err != nil {
			t.Fatalf("invalid-credential driver construction failed: %T", err)
		}
		defer wrong.Close(ctx) //nolint:errcheck
		session := wrong.NewSession(ctx, neo4jdriver.SessionConfig{DatabaseName: "neo4j", AccessMode: neo4jdriver.AccessModeRead})
		defer session.Close(ctx) //nolint:errcheck
		result, err := session.Run(ctx, "CALL db.info() YIELD id RETURN id", nil)
		if err == nil {
			_, err = result.Consume(ctx)
		}
		var refusal *neo4jdriver.Neo4jError
		if !errors.As(err, &refusal) || refusal.Code != "Neo.ClientError.Security.Unauthorized" {
			t.Fatalf("invalid credentials must be denied, not a missing identity: %T", err)
		}
	} else {
		baseline := loadIdentitySnapshot(t, directory, "baseline")
		switch phase {
		case "restart":
			if current != baseline {
				t.Fatal("same-store restart changed native identity or data")
			}
		case "post-dump":
			if current != baseline {
				t.Fatal("dump changed native identity or data")
			}
			seedIdentityFixture(t, ctx, driver, "post_dump")
			current = observeIdentity(t, ctx, driver)
			if !current.sameCandidate(baseline) || current.Original != 1 || current.PostDump != 1 {
				t.Fatal("post-dump fixture change not confirmed")
			}
		case "recreated", "clone-recreated":
			if current.ID == baseline.ID || current.SystemID == baseline.SystemID || current.CreationDate == baseline.CreationDate || current.Original != 0 || current.PostDump != 0 {
				t.Fatal("new owned storage did not create a distinct empty target")
			}
			saveIdentitySnapshot(t, directory, phase, current)
		case "restored", "restored-restart":
			recreated := loadIdentitySnapshot(t, directory, "recreated")
			if !current.sameCandidate(baseline) || current.ID == recreated.ID || current.SystemID == baseline.SystemID || current.SystemID != recreated.SystemID || current.Original != 1 || current.PostDump != 0 {
				t.Fatal("fixed-version restore identity counterexample changed; review native evidence")
			}
			if phase == "restored" {
				t.Log("NEO4J_IDENTITY_DATABASE_RESTORE database_candidate_preserved=true instance_identity_changed=true")
			}
		case "full-clone", "full-clone-restart":
			fresh := loadIdentitySnapshot(t, directory, "clone-recreated")
			if current != baseline || current.SystemID == fresh.SystemID {
				t.Fatal("full-store clone did not reproduce the observed identities and archived data")
			}
			if phase == "full-clone" {
				t.Logf("NEO4J_IDENTITY_COUNTEREXAMPLE candidate_preserved=true original_storage_destroyed=true post_dump_data_absent=true instance_identity_preserved=true database_id_available=%t", current.DatabaseID != "")
			}
		default:
			t.Fatal("unknown native identity lifecycle phase")
		}
	}
	t.Logf("NEO4J_IDENTITY_PHASE_PASS phase=%s version=%s edition=%s", phase, current.Version, current.Edition)
}
