package plugin

import (
	"context"
	"testing"
)

// MockPlugin 用于测试的模拟插件
type MockPlugin struct {
	TypeValue            string
	DisplayNameValue     string
	EngineOriginValue    string
	DefaultPortValue     int
	RequiredFieldsValue  []string
	SensitiveFieldsValue []string
}

func (m *MockPlugin) Type() string              { return m.TypeValue }
func (m *MockPlugin) DisplayName() string       { return m.DisplayNameValue }
func (m *MockPlugin) EngineOrigin() string      { return m.EngineOriginValue }
func (m *MockPlugin) DefaultPort() int          { return m.DefaultPortValue }
func (m *MockPlugin) RequiredFields() []string  { return m.RequiredFieldsValue }
func (m *MockPlugin) SensitiveFields() []string { return m.SensitiveFieldsValue }
func (m *MockPlugin) Capabilities() EngineCapabilities {
	return EngineCapabilities{
		SchemaVersion: CapabilitiesSchemaVersion,
		EngineType:    m.TypeValue,
		EngineFamily:  "test",
	}
}
func (m *MockPlugin) TestConnection(ctx context.Context, connInfo ConnectionInfo) error {
	return nil
}
func (m *MockPlugin) BuildDSN(connInfo ConnectionInfo) (string, error) {
	return "mock://connection", nil
}
func (m *MockPlugin) ValidateConnectionInfo(connInfo ConnectionInfo) error {
	return nil
}

func TestRegister(t *testing.T) {
	// 清理环境
	Clear()

	mockPlugin := &MockPlugin{
		TypeValue:        "mock_db",
		DisplayNameValue: "Mock Database",
	}

	Register(mockPlugin)

	// 验证注册成功
	plugin, err := Get("mock_db")
	if err != nil {
		t.Fatalf("Expected plugin to be registered, got error: %v", err)
	}

	if plugin.Type() != "mock_db" {
		t.Errorf("Expected type 'mock_db', got '%s'", plugin.Type())
	}
}

func TestRegisterNilPanic(t *testing.T) {
	Clear()

	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Expected panic when registering nil plugin")
		}
	}()

	Register(nil)
}

func TestRegisterEmptyTypePanic(t *testing.T) {
	Clear()

	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Expected panic when registering plugin with empty type")
		}
	}()

	mockPlugin := &MockPlugin{
		TypeValue: "",
	}
	Register(mockPlugin)
}

func TestGetCaseInsensitive(t *testing.T) {
	Clear()

	mockPlugin := &MockPlugin{
		TypeValue: "PostgreSQL",
	}
	Register(mockPlugin)

	// 测试不同大小写
	testCases := []string{"postgresql", "PostgreSQL", "POSTGRESQL", "PoStGrEsQl"}
	for _, tc := range testCases {
		plugin, err := Get(tc)
		if err != nil {
			t.Errorf("Expected to get plugin for '%s', got error: %v", tc, err)
		}
		if plugin == nil {
			t.Errorf("Expected plugin for '%s', got nil", tc)
		}
	}
}

func TestGetNonExistent(t *testing.T) {
	Clear()

	_, err := Get("nonexistent_db")
	if err == nil {
		t.Error("Expected error when getting non-existent plugin")
	}
}

func TestList(t *testing.T) {
	Clear()

	// 注册多个插件
	Register(&MockPlugin{TypeValue: "postgres"})
	Register(&MockPlugin{TypeValue: "mysql"})
	Register(&MockPlugin{TypeValue: "doris"})

	types := List()

	if len(types) != 3 {
		t.Errorf("Expected 3 types, got %d", len(types))
	}

	// 验证排序
	expected := []string{"doris", "mysql", "postgres"}
	for i, typ := range types {
		if typ != expected[i] {
			t.Errorf("Expected types[%d] = '%s', got '%s'", i, expected[i], typ)
		}
	}
}

func TestGetAll(t *testing.T) {
	Clear()

	Register(&MockPlugin{TypeValue: "postgres"})
	Register(&MockPlugin{TypeValue: "mysql"})

	plugins := GetAll()

	if len(plugins) != 2 {
		t.Errorf("Expected 2 plugins, got %d", len(plugins))
	}

	if _, ok := plugins["postgres"]; !ok {
		t.Error("Expected 'postgres' in plugins map")
	}

	if _, ok := plugins["mysql"]; !ok {
		t.Error("Expected 'mysql' in plugins map")
	}
}

func TestHas(t *testing.T) {
	Clear()

	Register(&MockPlugin{TypeValue: "postgres"})

	if !Has("postgres") {
		t.Error("Expected Has('postgres') to return true")
	}

	if !Has("POSTGRES") {
		t.Error("Expected Has('POSTGRES') to return true (case insensitive)")
	}

	if Has("mysql") {
		t.Error("Expected Has('mysql') to return false")
	}
}

func TestUnregister(t *testing.T) {
	Clear()

	Register(&MockPlugin{TypeValue: "postgres"})

	if !Has("postgres") {
		t.Error("Expected plugin to be registered")
	}

	Unregister("postgres")

	if Has("postgres") {
		t.Error("Expected plugin to be unregistered")
	}
}

func TestClear(t *testing.T) {
	// 注册多个插件
	Register(&MockPlugin{TypeValue: "postgres"})
	Register(&MockPlugin{TypeValue: "mysql"})
	Register(&MockPlugin{TypeValue: "doris"})

	if len(List()) != 3 {
		t.Error("Expected 3 plugins before clear")
	}

	Clear()

	if len(List()) != 0 {
		t.Error("Expected 0 plugins after clear")
	}
}

func TestRegisterOverwrite(t *testing.T) {
	Clear()

	plugin1 := &MockPlugin{
		TypeValue:        "postgres",
		DisplayNameValue: "PostgreSQL v1",
	}
	Register(plugin1)

	plugin2 := &MockPlugin{
		TypeValue:        "postgres",
		DisplayNameValue: "PostgreSQL v2",
	}
	Register(plugin2)

	retrieved, _ := Get("postgres")
	if retrieved.DisplayName() != "PostgreSQL v2" {
		t.Error("Expected newer plugin to overwrite older one")
	}
}
