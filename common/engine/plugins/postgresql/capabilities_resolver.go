package postgresql

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
)

type postgresExtensionFact struct {
	Name    string
	Version string
	Schema  string
}

type postgresInstanceCapabilityFacts struct {
	ServerVersion            string
	ServerVersionNum         int
	InstalledExtensions      map[string]postgresExtensionFact
	AvailableExtensions      map[string]string
	HasPostGISVersion        bool
	HasSTExtent              bool
	HasSTTransform           bool
	HasVectorType            bool
	SuperMapSystemTableCount int
	ArcGISSdeSchemaCount     int
	ArcGISSdeTableCount      int
}

const superMapSDXSystemTableThreshold = 3

func (p *PostgreSQLPlugin) ResolveCapabilities(ctx context.Context, connInfo plugin.ConnectionInfo, base plugin.EngineCapabilities) (plugin.EngineCapabilities, error) {
	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		return base, fmt.Errorf("build postgresql capability probe dsn: %w", err)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return base, fmt.Errorf("open postgresql capability probe connection: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	facts, err := queryPostgresInstanceCapabilityFacts(probeCtx, db)
	if err != nil {
		return base, err
	}
	return applyPostgresInstanceCapabilities(base, facts), nil
}

func queryPostgresInstanceCapabilityFacts(ctx context.Context, db *sql.DB) (postgresInstanceCapabilityFacts, error) {
	facts := postgresInstanceCapabilityFacts{
		InstalledExtensions: map[string]postgresExtensionFact{},
		AvailableExtensions: map[string]string{},
	}

	var serverVersionNum string
	if err := db.QueryRowContext(ctx, `SELECT current_setting('server_version'), current_setting('server_version_num')`).Scan(&facts.ServerVersion, &serverVersionNum); err != nil {
		return facts, fmt.Errorf("query postgresql server version: %w", err)
	}
	if parsed, err := strconv.Atoi(strings.TrimSpace(serverVersionNum)); err == nil {
		facts.ServerVersionNum = parsed
	}

	rows, err := db.QueryContext(ctx, `
		SELECT e.extname, e.extversion, n.nspname
		FROM pg_extension e
		JOIN pg_namespace n ON n.oid = e.extnamespace
		ORDER BY e.extname
	`)
	if err != nil {
		return facts, fmt.Errorf("query postgresql installed extensions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var fact postgresExtensionFact
		if err := rows.Scan(&fact.Name, &fact.Version, &fact.Schema); err != nil {
			return facts, fmt.Errorf("scan postgresql installed extension: %w", err)
		}
		facts.InstalledExtensions[strings.ToLower(strings.TrimSpace(fact.Name))] = fact
	}
	if err := rows.Err(); err != nil {
		return facts, fmt.Errorf("iterate postgresql installed extensions: %w", err)
	}

	availableRows, err := db.QueryContext(ctx, `
		SELECT name, default_version
		FROM pg_available_extensions
		WHERE name IN ('postgis', 'postgis_topology', 'postgis_tiger_geocoder', 'vector')
		ORDER BY name
	`)
	if err != nil {
		return facts, fmt.Errorf("query postgresql available extensions: %w", err)
	}
	defer availableRows.Close()
	for availableRows.Next() {
		var name, defaultVersion string
		if err := availableRows.Scan(&name, &defaultVersion); err != nil {
			return facts, fmt.Errorf("scan postgresql available extension: %w", err)
		}
		facts.AvailableExtensions[strings.ToLower(strings.TrimSpace(name))] = defaultVersion
	}
	if err := availableRows.Err(); err != nil {
		return facts, fmt.Errorf("iterate postgresql available extensions: %w", err)
	}

	if err := db.QueryRowContext(ctx, `
		SELECT
			EXISTS(SELECT 1 FROM pg_proc WHERE proname = 'postgis_version'),
			EXISTS(SELECT 1 FROM pg_proc WHERE proname = 'st_extent'),
			EXISTS(SELECT 1 FROM pg_proc WHERE proname = 'st_transform'),
			EXISTS(SELECT 1 FROM pg_type WHERE typname = 'vector'),
			COALESCE((
				SELECT COUNT(*)
				FROM information_schema.tables
				WHERE table_schema NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
				  AND lower(table_name) IN (`+superMapSDXSystemTableSQLList()+`)
			), 0),
			COALESCE((
				SELECT COUNT(*)
				FROM information_schema.schemata
				WHERE lower(schema_name) = 'sde'
			), 0),
			COALESCE((
				SELECT COUNT(*)
				FROM information_schema.tables
				WHERE lower(table_schema) = 'sde'
				   OR lower(table_name) IN (
						'sde_table_registry',
						'sde_layer_registry',
						'sde_layers',
						'sde_states',
						'gdb_items',
						'gdb_itemtypes',
						'gdb_itemrelationships'
				   )
			), 0)
		`).Scan(
		&facts.HasPostGISVersion,
		&facts.HasSTExtent,
		&facts.HasSTTransform,
		&facts.HasVectorType,
		&facts.SuperMapSystemTableCount,
		&facts.ArcGISSdeSchemaCount,
		&facts.ArcGISSdeTableCount,
	); err != nil {
		return facts, fmt.Errorf("query postgresql extension functions: %w", err)
	}

	return facts, nil
}

func applyPostgresInstanceCapabilities(base plugin.EngineCapabilities, facts postgresInstanceCapabilityFacts) plugin.EngineCapabilities {
	postgisFact, hasPostGIS := facts.InstalledExtensions["postgis"]
	postgisReady := hasPostGIS && facts.HasPostGISVersion && facts.HasSTExtent
	transformReady := postgisReady && facts.HasSTTransform
	pgvectorState := extensionState(facts, "vector")
	if installed, _ := pgvectorState["installed"].(bool); !installed && facts.HasVectorType {
		pgvectorState["installed"] = true
	}
	pgvectorState["type_available"] = facts.HasVectorType

	if base.Storage != nil {
		if base.Storage.Facts != nil {
			base.Storage.Facts.SpatialFacts = postgisReady
		}
		if base.Storage.Store != nil {
			base.Storage.Store.TableReadSpatialTransform = transformReady
			if !postgisReady {
				base.Storage.Store.TableSpatialEncoding = nil
			} else if base.Storage.Store.TableSpatialEncoding != nil {
				base.Storage.Store.TableSpatialEncoding.ReadTransform = transformReady
				base.Storage.Store.TableSpatialEncoding.NativeSpatialFunctions = postgisReady
			}
		}
	}

	extensions := map[string]interface{}{
		"postgresql": map[string]interface{}{
			"server_version":     facts.ServerVersion,
			"server_version_num": facts.ServerVersionNum,
			"postgis": map[string]interface{}{
				"installed":    hasPostGIS,
				"available":    extensionAvailable(facts, "postgis"),
				"version":      postgisFact.Version,
				"schema":       postgisFact.Schema,
				"st_extent":    facts.HasSTExtent,
				"st_transform": facts.HasSTTransform,
			},
			"postgis_topology": extensionState(facts, "postgis_topology"),
			"pgvector":         pgvectorState,
		},
	}

	if tiger, ok := facts.InstalledExtensions["postgis_tiger_geocoder"]; ok {
		extensions["postgresql"].(map[string]interface{})["postgis_tiger_geocoder"] = map[string]interface{}{
			"installed": true,
			"available": extensionAvailable(facts, "postgis_tiger_geocoder"),
			"version":   tiger.Version,
			"schema":    tiger.Schema,
		}
	}

	workspaceKind := plugin.SpatialWorkspaceSuperMapSDXPostgreSQL
	if postgisReady {
		workspaceKind = plugin.SpatialWorkspaceSuperMapSDXPostGIS
	}
	workspaceDetected := facts.SuperMapSystemTableCount >= superMapSDXSystemTableThreshold
	workspaceState := plugin.SpatialWorkspaceStateNotDetected
	if workspaceDetected {
		workspaceState = plugin.SpatialWorkspaceStateDetected
	}
	workspaces := []plugin.SpatialWorkspaceFact{
		{
			Ecosystem:         "supermap",
			Kind:              workspaceKind,
			State:             workspaceState,
			BackendEngineType: "postgresql",
			RuntimeEngineType: "supermap_workflow",
			CanEnable:         !workspaceDetected,
			RiskLevel:         plugin.SpatialWorkspaceRiskHigh,
			Evidence: map[string]interface{}{
				"postgis_ready":               postgisReady,
				"supermap_system_table_count": facts.SuperMapSystemTableCount,
			},
		},
	}
	if facts.ArcGISSdeSchemaCount > 0 || facts.ArcGISSdeTableCount > 0 {
		workspaces = append(workspaces, plugin.SpatialWorkspaceFact{
			Ecosystem:         "arcgis",
			Kind:              "sde",
			State:             spatialWorkspaceState(false, facts.ArcGISSdeSchemaCount+facts.ArcGISSdeTableCount, 1),
			BackendEngineType: "postgresql",
			CanEnable:         false,
			RiskLevel:         plugin.SpatialWorkspaceRiskHigh,
			Evidence: map[string]interface{}{
				"sde_schema_count": facts.ArcGISSdeSchemaCount,
				"sde_table_count":  facts.ArcGISSdeTableCount,
			},
		})
	}
	extensions[plugin.EngineExtensionSpatialWorkspaces] = workspaces

	base.Extensions = extensions

	return base
}

func spatialWorkspaceState(_ bool, evidenceCount int, threshold int) string {
	if evidenceCount >= threshold {
		return plugin.SpatialWorkspaceStateDetected
	}
	return plugin.SpatialWorkspaceStateNotDetected
}

func extensionAvailable(facts postgresInstanceCapabilityFacts, name string) bool {
	_, ok := facts.AvailableExtensions[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

func extensionState(facts postgresInstanceCapabilityFacts, name string) map[string]interface{} {
	normalized := strings.ToLower(strings.TrimSpace(name))
	fact, installed := facts.InstalledExtensions[normalized]
	return map[string]interface{}{
		"installed": installed,
		"available": extensionAvailable(facts, normalized),
		"version":   fact.Version,
		"schema":    fact.Schema,
	}
}
