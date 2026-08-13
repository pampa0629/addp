package oracle

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
)

var oracleSDERequiredRegistryTables = []string{
	"TABLE_REGISTRY",
	"GDB_ITEMS",
	"GDB_ITEMTYPES",
	"GEOMETRY_COLUMNS",
}

const oracleSDERepositoryOwner = "SDE"

var oracleSDEVersioningTables = []string{
	"STATES",
	"STATE_LINEAGES",
	"VERSIONS",
}

var oracleSDEFeatureRegistryTables = []string{
	"LAYERS",
	"COLUMN_REGISTRY",
}

type oracleSDEOwnerFacts struct {
	Owner  string
	Tables map[string]struct{}
}

func (p *OraclePlugin) ResolveCapabilities(ctx context.Context, connInfo plugin.ConnectionInfo, base plugin.EngineCapabilities) (plugin.EngineCapabilities, error) {
	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		return base, fmt.Errorf("build Oracle capability probe DSN: %w", err)
	}
	db, err := sql.Open("oracle", dsn)
	if err != nil {
		return base, fmt.Errorf("open Oracle capability probe connection: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	facts, err := queryOracleSDEOwnerFacts(probeCtx, db)
	if err != nil {
		if isOraclePermissionError(err) {
			return applyOracleSDEWorkspaceCapability(base, nil, true), nil
		}
		return base, err
	}
	if owner, requiredCount := selectOracleSDERepositoryOwner(facts); owner != nil && requiredCount == len(oracleSDERequiredRegistryTables) {
		if err := probeOracleSDERepositoryAccess(probeCtx, db, owner); err != nil {
			if isOraclePermissionError(err) {
				return applyOracleSDEWorkspaceCapability(base, facts, true), nil
			}
			return base, err
		}
	}
	return applyOracleSDEWorkspaceCapability(base, facts, false), nil
}

func isOraclePermissionError(err error) bool {
	message := strings.ToLower(err.Error())
	for _, marker := range []string{"ora-00942", "ora-01031", "insufficient privileges", "permission denied", "table or view does not exist"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func probeOracleSDERepositoryAccess(ctx context.Context, db *sql.DB, owner *oracleSDEOwnerFacts) error {
	if owner == nil {
		return nil
	}
	for _, table := range oracleSDERequiredRegistryTables {
		var count int64
		query := fmt.Sprintf(`SELECT COUNT(*) FROM %s.%s WHERE 1 = 0`, quoteOracleIdentifier(owner.Owner), quoteOracleIdentifier(table))
		if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil {
			return fmt.Errorf("probe Oracle ArcGIS SDE repository table %s.%s: %w", owner.Owner, table, err)
		}
	}
	return nil
}

func quoteOracleIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(strings.TrimSpace(identifier), `"`, `""`) + `"`
}

func queryOracleSDEOwnerFacts(ctx context.Context, db *sql.DB) ([]oracleSDEOwnerFacts, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT owner, table_name
		  FROM all_tables
		 WHERE owner = 'SDE'
		   AND table_name IN (
		       'TABLE_REGISTRY', 'GDB_ITEMS', 'GDB_ITEMTYPES', 'GEOMETRY_COLUMNS',
		       'STATES', 'STATE_LINEAGES', 'VERSIONS', 'LAYERS', 'COLUMN_REGISTRY'
		 )
		 ORDER BY owner, table_name
	`)
	if err != nil {
		return nil, fmt.Errorf("query Oracle ArcGIS SDE repository tables: %w", err)
	}
	defer rows.Close()

	byOwner := map[string]map[string]struct{}{}
	for rows.Next() {
		var owner, table string
		if err := rows.Scan(&owner, &table); err != nil {
			return nil, fmt.Errorf("scan Oracle ArcGIS SDE repository table: %w", err)
		}
		owner = strings.ToUpper(strings.TrimSpace(owner))
		table = strings.ToUpper(strings.TrimSpace(table))
		if owner == "" || table == "" {
			continue
		}
		if byOwner[owner] == nil {
			byOwner[owner] = map[string]struct{}{}
		}
		byOwner[owner][table] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Oracle ArcGIS SDE repository tables: %w", err)
	}

	owners := make([]string, 0, len(byOwner))
	for owner := range byOwner {
		owners = append(owners, owner)
	}
	sort.Strings(owners)
	facts := make([]oracleSDEOwnerFacts, 0, len(owners))
	for _, owner := range owners {
		facts = append(facts, oracleSDEOwnerFacts{Owner: owner, Tables: byOwner[owner]})
	}
	return facts, nil
}

func applyOracleSDEWorkspaceCapability(base plugin.EngineCapabilities, owners []oracleSDEOwnerFacts, permissionDenied bool) plugin.EngineCapabilities {
	state := plugin.SpatialWorkspaceStateNotDetected
	evidence := map[string]interface{}{
		"required_registry_tables": append([]string(nil), oracleSDERequiredRegistryTables...),
		"required_registry_count":  0,
		"versioning_table_count":   0,
		"feature_registry_count":   0,
	}

	if permissionDenied {
		state = plugin.SpatialWorkspaceStatePermissionDenied
		evidence["probe_permission_denied"] = true
	} else if owner, requiredCount := selectOracleSDERepositoryOwner(owners); owner != nil {
		evidence["repository_owner"] = owner.Owner
		evidence["required_registry_count"] = requiredCount
		evidence["versioning_table_count"] = oracleSDETableCount(owner.Tables, oracleSDEVersioningTables)
		evidence["feature_registry_count"] = oracleSDETableCount(owner.Tables, oracleSDEFeatureRegistryTables)
		evidence["versioned_repository_detected"] = oracleSDETableCount(owner.Tables, oracleSDEVersioningTables) == len(oracleSDEVersioningTables)
		if requiredCount == len(oracleSDERequiredRegistryTables) {
			state = plugin.SpatialWorkspaceStateDetected
		}
	}

	plugin.SetSpatialWorkspacesExtension(&base, []plugin.SpatialWorkspaceFact{{
		Ecosystem:         "arcgis",
		Kind:              plugin.SpatialWorkspaceArcGISSDE,
		State:             state,
		BackendEngineType: "oracle",
		CanEnable:         false,
		RiskLevel:         plugin.SpatialWorkspaceRiskHigh,
		Evidence:          evidence,
	}})
	return base
}

func selectOracleSDERepositoryOwner(owners []oracleSDEOwnerFacts) (*oracleSDEOwnerFacts, int) {
	bestCount := 0
	var best *oracleSDEOwnerFacts
	for i := range owners {
		if owners[i].Owner != oracleSDERepositoryOwner {
			continue
		}
		count := oracleSDETableCount(owners[i].Tables, oracleSDERequiredRegistryTables)
		if count > bestCount || (count == bestCount && count > 0 && best != nil && owners[i].Owner < best.Owner) {
			best = &owners[i]
			bestCount = count
		}
	}
	return best, bestCount
}

func oracleSDETableCount(actual map[string]struct{}, expected []string) int {
	count := 0
	for _, name := range expected {
		if _, ok := actual[name]; ok {
			count++
		}
	}
	return count
}
