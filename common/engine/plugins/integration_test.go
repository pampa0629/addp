package plugins_test

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"

	// 导入所有插件以触发 init() 注册
	_ "github.com/addp/common/engine/plugins/builtin/all"
)

func TestAllPluginsRegistered(t *testing.T) {
	expectedTypes := []string{
		"clickhouse",
		"doris",
		"duckdb",
		"jupyter",
		"kafka",
		"minio",
		"mongodb",
		"mysql",
		"neo4j",
		"nfs",
		"postgresql",
		"inference_runtime",
		"s3",
		"spark",
	}

	registeredTypes := plugin.List()
	sort.Strings(registeredTypes)
	sort.Strings(expectedTypes)

	if len(registeredTypes) != len(expectedTypes) {
		t.Errorf("Expected %d plugins, got %d", len(expectedTypes), len(registeredTypes))
		t.Logf("Expected: %v", expectedTypes)
		t.Logf("Registered: %v", registeredTypes)
		return
	}

	for i, expected := range expectedTypes {
		if registeredTypes[i] != expected {
			t.Errorf("Expected plugin '%s' at index %d, got '%s'", expected, i, registeredTypes[i])
		}
	}
}

func TestGetAllPlugins(t *testing.T) {
	plugins := plugin.GetAll()

	if len(plugins) != 14 {
		t.Errorf("Expected 14 plugins, got %d", len(plugins))
	}

	// 验证每个插件的基本信息
	testCases := []struct {
		dbType   string
		expected string
	}{
		{"postgresql", "PostgreSQL"},
		{"mysql", "MySQL"},
		{"doris", "Apache Doris"},
		{"spark", "Apache Spark"},
		{"clickhouse", "ClickHouse"},
		{"duckdb", "DuckDB 联邦查询 Runtime"},
		{"jupyter", "Jupyter Engine"},
		{"inference_runtime", "ADDP AI Inference Runtime"},
		{"kafka", "Apache Kafka"},
		{"mongodb", "MongoDB"},
		{"neo4j", "Neo4j"},
		{"nfs", "NFS 文件系统"},
		{"minio", "MinIO"},
		{"s3", "Amazon S3"},
	}

	for _, tc := range testCases {
		p, err := plugin.Get(tc.dbType)
		if err != nil {
			t.Errorf("Failed to get plugin for '%s': %v", tc.dbType, err)
			continue
		}

		if p.DisplayName() != tc.expected {
			t.Errorf("Expected display name '%s' for '%s', got '%s'",
				tc.expected, tc.dbType, p.DisplayName())
		}
	}
}

func TestPluginCapabilities(t *testing.T) {
	testCases := []struct {
		dbType string
		origin string
	}{
		{"postgresql", "general"},
		{"mysql", "general"},
		{"doris", "general"},
		{"clickhouse", "general"},
		{"mongodb", "general"},
		{"kafka", "general"},
		{"spark", "general"},
		{"minio", "general"},
		{"s3", "general"},
		{"nfs", "general"},
		{"neo4j", "general"},
		{"duckdb", "extension"},
		{"jupyter", "extension"},
	}

	for _, tc := range testCases {
		p, err := plugin.Get(tc.dbType)
		if err != nil {
			t.Errorf("Failed to get plugin for '%s': %v", tc.dbType, err)
			continue
		}

		if p.EngineOrigin() != tc.origin {
			t.Errorf("Expected origin '%s' for '%s', got '%s'",
				tc.origin, tc.dbType, p.EngineOrigin())
		}

		// 验证能力声明不为空
		capabilities, err := plugin.GenerateCapabilities(tc.dbType)
		if err != nil {
			t.Errorf("Plugin '%s' failed to generate capabilities: %v", tc.dbType, err)
			continue
		}
		if capabilities == "" {
			t.Errorf("Plugin '%s' returned empty capabilities", tc.dbType)
		}
	}
}

func TestPluginDefaultPorts(t *testing.T) {
	testCases := []struct {
		dbType      string
		defaultPort int
	}{
		{"postgresql", 5432},
		{"mysql", 3306},
		{"doris", 9030},
		{"clickhouse", 9000},
		{"mongodb", 27017},
		{"kafka", 9092},
		{"spark", 10000},
		{"minio", 9000},
		{"s3", 443},
		{"nfs", 2049},
		{"neo4j", 7687},
		{"duckdb", 8104},
		{"jupyter", 8097},
	}

	for _, tc := range testCases {
		p, err := plugin.Get(tc.dbType)
		if err != nil {
			t.Errorf("Failed to get plugin for '%s': %v", tc.dbType, err)
			continue
		}

		if p.DefaultPort() != tc.defaultPort {
			t.Errorf("Expected default port %d for '%s', got %d",
				tc.defaultPort, tc.dbType, p.DefaultPort())
		}
	}
}

func TestPluginRequiredFields(t *testing.T) {
	testCases := []struct {
		dbType   string
		hasField string
	}{
		{"postgresql", "host"},
		{"mysql", "user"},
		{"doris", "database"},
		{"clickhouse", "host"},
		{"mongodb", "host"},
		{"kafka", "bootstrap_servers"},
		{"spark", "host"},
		{"minio", "endpoint"},
		{"s3", "access_key"},
		{"nfs", "server"},
		{"neo4j", "password"},
		{"duckdb", "host"},
		{"jupyter", "port"},
	}

	for _, tc := range testCases {
		p, err := plugin.Get(tc.dbType)
		if err != nil {
			t.Errorf("Failed to get plugin for '%s': %v", tc.dbType, err)
			continue
		}

		requiredFields := p.RequiredFields()
		found := false
		for _, field := range requiredFields {
			if field == tc.hasField {
				found = true
				break
			}
		}

		if !found {
			t.Errorf("Expected required field '%s' for '%s', not found in %v",
				tc.hasField, tc.dbType, requiredFields)
		}
	}
}

func TestPluginSensitiveFields(t *testing.T) {
	allPlugins := plugin.GetAll()

	for dbType, p := range allPlugins {
		if p.EngineOrigin() == "extension" || dbType == "nfs" {
			continue
		}
		sensitiveFields := p.SensitiveFields()
		if len(sensitiveFields) == 0 {
			t.Errorf("Plugin '%s' has no sensitive fields defined", dbType)
		}
	}
}

func TestPluginCapabilitiesMatchProviders(t *testing.T) {
	for engineType, p := range plugin.GetAll() {
		t.Run(engineType, func(t *testing.T) {
			if err := plugin.ValidatePluginCapabilities(p); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBuiltinPluginCapabilityMatrix(t *testing.T) {
	testCases := map[string]struct {
		origin          string
		family          string
		storage         bool
		query           bool
		workflow        bool
		script          bool
		inference       bool
		graphQuery      bool
		workflowRuntime string
		scriptModes     []string
		scriptLanguages []string
	}{
		"clickhouse":        {origin: "general", family: "tabular", storage: true, query: true},
		"doris":             {origin: "general", family: "tabular", storage: true, query: true},
		"kafka":             {origin: "general", family: "event_stream", storage: true},
		"mongodb":           {origin: "general", family: "dynamic_schema", storage: true, query: true},
		"mysql":             {origin: "general", family: "tabular", storage: true, query: true},
		"neo4j":             {origin: "general", family: "graph", storage: true, query: true, graphQuery: true},
		"postgresql":        {origin: "general", family: "tabular", storage: true, query: true},
		"spark":             {origin: "general", family: "tabular", storage: true, query: true},
		"minio":             {origin: "general", family: "object", storage: true},
		"nfs":               {origin: "general", family: "file", storage: true},
		"s3":                {origin: "general", family: "object", storage: true},
		"duckdb":            {origin: "extension", family: "query_runtime", query: true},
		"jupyter":           {origin: "extension", family: "script", script: true, scriptModes: []string{"notebook"}, scriptLanguages: []string{"python"}},
		"inference_runtime": {origin: "extension", family: "inference", inference: true},
	}

	allPlugins := plugin.GetAll()
	if len(allPlugins) != len(testCases) {
		t.Fatalf("expected %d builtin plugins in capability matrix, got %d", len(testCases), len(allPlugins))
	}

	for engineType, expected := range testCases {
		t.Run(engineType, func(t *testing.T) {
			p, err := plugin.Get(engineType)
			if err != nil {
				t.Fatalf("get plugin: %v", err)
			}
			caps := p.Capabilities()
			if p.EngineOrigin() != expected.origin {
				t.Fatalf("origin = %q, want %q", p.EngineOrigin(), expected.origin)
			}
			if caps.EngineFamily != expected.family {
				t.Fatalf("engine_family = %q, want %q", caps.EngineFamily, expected.family)
			}
			if hasStorage(caps) != expected.storage {
				t.Fatalf("storage support = %v, want %v", hasStorage(caps), expected.storage)
			}
			if hasQuery(caps) != expected.query {
				t.Fatalf("query support = %v, want %v", hasQuery(caps), expected.query)
			}
			if hasWorkflow(caps) != expected.workflow {
				t.Fatalf("workflow support = %v, want %v", hasWorkflow(caps), expected.workflow)
			}
			if hasScript(caps) != expected.script {
				t.Fatalf("script support = %v, want %v", hasScript(caps), expected.script)
			}
			if hasInference(caps) != expected.inference {
				t.Fatalf("inference support = %v, want %v", hasInference(caps), expected.inference)
			}
			if expected.graphQuery && !contains(caps.Compute.Query.ResultKinds, "graph") {
				t.Fatalf("graph plugin must declare graph query result kind")
			}
			if expected.workflowRuntime != "" && caps.Compute.Workflow.RuntimeAPI != expected.workflowRuntime {
				t.Fatalf("workflow runtime_api = %q, want %q", caps.Compute.Workflow.RuntimeAPI, expected.workflowRuntime)
			}
			for _, mode := range expected.scriptModes {
				if !contains(caps.Compute.Script.Modes, mode) {
					t.Fatalf("script modes %v do not include %q", caps.Compute.Script.Modes, mode)
				}
			}
			for _, language := range expected.scriptLanguages {
				if !contains(caps.Compute.Script.Languages, language) {
					t.Fatalf("script languages %v do not include %q", caps.Compute.Script.Languages, language)
				}
			}
		})
	}
}

func hasInference(caps plugin.EngineCapabilities) bool {
	return caps.Compute != nil && caps.Compute.Inference != nil && caps.Compute.Inference.Supported
}

func TestWorkflowRuntimeRegistrationIncludesCapabilities(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate test file")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "../../.."))

	testCases := []struct {
		name     string
		path     string
		required []string
	}{
		{
			name: "geopython_workflow",
			path: "engines/python-workflow/api_server.py",
			required: []string{
				`"engine_type": "geopython_workflow"`,
				`"schema_version": "engine.capabilities/v1"`,
				`"runtime_api": "addp.workflow/v1"`,
				`"is_builtin": True`,
			},
		},
		{
			name: "spark_workflow",
			path: "engines/spark-workflow/api_server.py",
			required: []string{
				`"engine_type": "spark_workflow"`,
				`"schema_version": "engine.capabilities/v1"`,
				`"runtime_api": "addp.workflow/v1"`,
				`"is_builtin": True`,
			},
		},
		{
			name: "math_workflow",
			path: "engines/math-workflow/api_server.py",
			required: []string{
				`"engine_type": "math_workflow"`,
				`"schema_version": "engine.capabilities/v1"`,
				`"runtime_api": "addp.workflow/v1"`,
				`"is_builtin": True`,
			},
		},
		{
			name: "model3d_workflow",
			path: "engines/model3d-workflow/api_server.py",
			required: []string{
				`"engine_type": "model3d_workflow"`,
				`"schema_version": "engine.capabilities/v1"`,
				`"runtime_api": "addp.workflow/v1"`,
				`"is_builtin": True`,
			},
		},
		{
			name: "pointcloud_workflow",
			path: "engines/pointcloud-workflow/api_server.py",
			required: []string{
				`"engine_type": "pointcloud_workflow"`,
				`"schema_version": "engine.capabilities/v1"`,
				`"runtime_api": "addp.workflow/v1"`,
				`"is_builtin": True`,
			},
		},
		{
			name: "supermap_workflow",
			path: "scripts/dev/supermap-workflow.sh",
			required: []string{
				`\"engine_type\":\"supermap_workflow\"`,
				`\"schema_version\":\"engine.capabilities/v1\"`,
				`\"runtime_api\":\"addp.workflow/v1\"`,
				`\"is_builtin\":true`,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			contentBytes, err := os.ReadFile(filepath.Join(repoRoot, tc.path))
			if err != nil {
				t.Fatalf("read runtime registration file: %v", err)
			}
			content := string(contentBytes)
			for _, fragment := range tc.required {
				if !strings.Contains(content, fragment) {
					t.Fatalf("%s missing canonical capability fragment %s", tc.path, fragment)
				}
			}
		})
	}
}

func hasStorage(caps plugin.EngineCapabilities) bool {
	return caps.Storage != nil
}

func hasQuery(caps plugin.EngineCapabilities) bool {
	return caps.Compute != nil && caps.Compute.Query != nil && caps.Compute.Query.Supported
}

func hasWorkflow(caps plugin.EngineCapabilities) bool {
	return caps.Compute != nil && caps.Compute.Workflow != nil && caps.Compute.Workflow.Supported
}

func hasScript(caps plugin.EngineCapabilities) bool {
	return caps.Compute != nil && caps.Compute.Script != nil && caps.Compute.Script.Supported
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
