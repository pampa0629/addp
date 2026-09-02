package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	commoninference "github.com/addp/common/inference"
	secretcipher "github.com/addp/common/secretcipher"
	"github.com/addp/inference/internal/models"
	"github.com/addp/inference/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var testEncryptionKey = []byte("0123456789abcdef0123456789abcdef")

func newTestStore(t *testing.T) *repository.Store {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS inference").Error; err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`CREATE TABLE inference.provider_connections (id TEXT PRIMARY KEY, name TEXT NOT NULL, scope_type TEXT NOT NULL, tenant_id INTEGER, adapter_type TEXT NOT NULL, endpoint TEXT NOT NULL, allow_all_tenants INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL, credential_ciphertext TEXT, credential_version INTEGER NOT NULL DEFAULT 0, created_by INTEGER NOT NULL, updated_by INTEGER NOT NULL, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE inference.provider_tenant_grants (provider_connection_id TEXT NOT NULL, tenant_id INTEGER NOT NULL, created_at DATETIME, PRIMARY KEY (provider_connection_id, tenant_id))`,
		`CREATE TABLE inference.model_deployments (id TEXT PRIMARY KEY, provider_connection_id TEXT NOT NULL, name TEXT NOT NULL, upstream_model TEXT NOT NULL, operations JSON NOT NULL, modalities JSON NOT NULL, dimension INTEGER NOT NULL DEFAULT 0, chat_max_output_tokens_parameter TEXT NOT NULL DEFAULT 'max_tokens', chat_temperature_mode TEXT NOT NULL DEFAULT 'configurable', status TEXT NOT NULL, created_by INTEGER NOT NULL, updated_by INTEGER NOT NULL, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE inference.model_profiles (id TEXT PRIMARY KEY, name TEXT NOT NULL, code TEXT NOT NULL, scope_type TEXT NOT NULL, tenant_id INTEGER, model_deployment_id TEXT NOT NULL, status TEXT NOT NULL, version INTEGER NOT NULL DEFAULT 1, created_by INTEGER NOT NULL, updated_by INTEGER NOT NULL, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE inference.credential_audits (id TEXT PRIMARY KEY, provider_connection_id TEXT NOT NULL, old_version INTEGER NOT NULL, new_version INTEGER NOT NULL, action TEXT NOT NULL, principal_id INTEGER NOT NULL, created_at DATETIME)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	return repository.NewStore(db)
}

func TestProviderScopeAndCredentialRotation(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	control := NewControlPlane(store, testEncryptionKey)
	platform := Actor{ContextType: models.ScopePlatform, PrincipalID: 11}
	tenant := Actor{ContextType: models.ScopeTenant, TenantID: 7, PrincipalID: 12}

	provider, err := control.CreateProvider(ctx, platform, ProviderInput{
		Name: "shared", ScopeType: models.ScopePlatform, AdapterType: AdapterOpenAICompatible,
		Endpoint: "https://example.test/v1", AllowedTenantIDs: []uint{7},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := control.GetProvider(ctx, tenant, provider.ID); err != nil {
		t.Fatalf("allowlisted tenant cannot read provider: %v", err)
	}
	if _, err := control.UpdateProvider(ctx, tenant, provider.ID, ProviderInput{}); err != ErrNotFound {
		t.Fatalf("tenant must not manage platform provider, got %v", err)
	}

	const secret = "test-secret-value"
	status, err := control.SetCredential(ctx, platform, provider.ID, secret)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Configured || status.Version != 1 {
		t.Fatalf("unexpected credential status: %+v", status)
	}
	stored, err := store.GetProvider(ctx, provider.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if stored.CredentialCiphertext == "" || stored.CredentialCiphertext == secret || strings.Contains(stored.CredentialCiphertext, secret) {
		t.Fatal("credential was not stored exclusively as ciphertext")
	}
	plaintext, err := secretcipher.Decrypt(stored.CredentialCiphertext, testEncryptionKey)
	if err != nil || plaintext != secret {
		t.Fatalf("credential ciphertext cannot be decrypted: %v", err)
	}
	encoded, err := json.Marshal(provider)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) || strings.Contains(string(encoded), stored.CredentialCiphertext) {
		t.Fatal("provider response exposed credential material")
	}
	var audits []models.CredentialAudit
	if err := store.DB().Find(&audits).Error; err != nil {
		t.Fatal(err)
	}
	if len(audits) != 1 || audits[0].OldVersion != 0 || audits[0].NewVersion != 1 {
		t.Fatalf("unexpected credential audit: %+v", audits)
	}

	deleted, err := control.DeleteCredential(ctx, platform, provider.ID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Configured || deleted.Version != 2 {
		t.Fatalf("unexpected deleted credential status: %+v", deleted)
	}
}

func TestTenantControlPlaneOnlyProjectsAuthorizedPlatformResources(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	control := NewControlPlane(store, testEncryptionKey)
	platform := Actor{ContextType: models.ScopePlatform, PrincipalID: 31}
	tenant := Actor{ContextType: models.ScopeTenant, TenantID: 7, PrincipalID: 32}

	createResource := func(name string, tenantID uint) (*ProviderView, *models.ModelDeployment, *models.ModelProfile) {
		t.Helper()
		provider, err := control.CreateProvider(ctx, platform, ProviderInput{
			Name: name, ScopeType: models.ScopePlatform, AdapterType: AdapterOpenAICompatible,
			Endpoint: "https://" + name + ".example.test/v1", AllowedTenantIDs: []uint{tenantID},
		})
		if err != nil {
			t.Fatal(err)
		}
		deployment, err := control.CreateDeployment(ctx, platform, DeploymentInput{
			ProviderConnectionID: provider.ID, Name: name, UpstreamModel: name,
			Operations: []string{"chat"}, Modalities: []string{"text"},
		})
		if err != nil {
			t.Fatal(err)
		}
		profile, err := control.CreateProfile(ctx, platform, ProfileInput{
			Name: name, Code: name, ScopeType: models.ScopePlatform, ModelDeploymentID: deployment.ID,
		})
		if err != nil {
			t.Fatal(err)
		}
		return provider, deployment, profile
	}

	allowedProvider, allowedDeployment, allowedProfile := createResource("allowed", 7)
	deniedProvider, deniedDeployment, deniedProfile := createResource("denied", 8)

	providers, err := control.ListProviders(ctx, tenant, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if providers.Total != 1 || providers.Data[0].ID != allowedProvider.ID {
		t.Fatalf("unexpected provider projection: %+v", providers)
	}
	deployments, err := control.ListDeployments(ctx, tenant, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if deployments.Total != 1 || deployments.Data[0].ID != allowedDeployment.ID {
		t.Fatalf("unexpected deployment projection: %+v", deployments)
	}
	profiles, err := control.ListProfiles(ctx, tenant, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if profiles.Total != 1 || profiles.Data[0].ID != allowedProfile.ID {
		t.Fatalf("unexpected profile projection: %+v", profiles)
	}

	if _, err := control.GetProvider(ctx, tenant, deniedProvider.ID); err != ErrNotFound {
		t.Fatalf("unauthorized provider must be hidden, got %v", err)
	}
	if _, err := control.GetDeployment(ctx, tenant, deniedDeployment.ID); err != ErrNotFound {
		t.Fatalf("unauthorized deployment must be hidden, got %v", err)
	}
	if _, err := control.GetProfile(ctx, tenant, deniedProfile.ID); err != ErrNotFound {
		t.Fatalf("unauthorized profile must be hidden, got %v", err)
	}
}

func TestRuntimeChatPreservesBasePathAndDoesNotFallback(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected upstream path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer runtime-secret" {
			t.Errorf("unexpected authorization header")
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body[ChatMaxOutputTokensParameterMaxCompletionTokens] != float64(64) {
			t.Errorf("max_completion_tokens = %#v, want 64", body[ChatMaxOutputTokensParameterMaxCompletionTokens])
		}
		if _, exists := body[ChatMaxOutputTokensParameterMaxTokens]; exists {
			t.Error("runtime sent both chat max output token parameters")
		}
		if _, exists := body["temperature"]; exists {
			t.Error("runtime sent temperature for a default-only deployment")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`))
	}))
	defer upstream.Close()

	ctx := context.Background()
	store := newTestStore(t)
	control := NewControlPlane(store, testEncryptionKey)
	platform := Actor{ContextType: models.ScopePlatform, PrincipalID: 21}
	provider, err := control.CreateProvider(ctx, platform, ProviderInput{Name: "openai", ScopeType: models.ScopePlatform, AdapterType: AdapterOpenAICompatible, Endpoint: upstream.URL + "/v1", AllowAllTenants: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := control.SetCredential(ctx, platform, provider.ID, "runtime-secret"); err != nil {
		t.Fatal(err)
	}
	deployment, err := control.CreateDeployment(ctx, platform, DeploymentInput{ProviderConnectionID: provider.ID, Name: "chat", UpstreamModel: "model-a", Operations: []string{"chat"}, Modalities: []string{"text"}, ChatMaxOutputTokensParameter: ChatMaxOutputTokensParameterMaxCompletionTokens, ChatTemperatureMode: ChatTemperatureModeDefaultOnly})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := control.CreateProfile(ctx, platform, ProfileInput{Name: "default chat", Code: "default-chat", ScopeType: models.ScopePlatform, ModelDeploymentID: deployment.ID})
	if err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(store, testEncryptionKey)
	temperature := 0.3
	response, err := runtime.Chat(ctx, commoninference.ChatRequest{SchemaVersion: commoninference.SchemaVersion, TenantID: 8, ModelProfileID: profile.ID, Messages: []commoninference.Message{{Role: "user", Content: "hello"}}, Temperature: &temperature, MaxOutputTokens: 64})
	if err != nil {
		t.Fatal(err)
	}
	if response.Message.Content != "ok" || response.DeploymentID != deployment.ID || calls.Load() != 1 {
		t.Fatalf("unexpected inference response: %+v, calls=%d", response, calls.Load())
	}
}

func TestDiscoverModelsUsesProviderCredentialAndPreservesBasePath(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			t.Errorf("unexpected discovery request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer discovery-secret" {
			t.Errorf("unexpected authorization header")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"model-b","owned_by":"vendor"},{"id":"model-a"},{"id":"model-b"}]}`))
	}))
	defer upstream.Close()

	ctx := context.Background()
	store := newTestStore(t)
	control := NewControlPlane(store, testEncryptionKey)
	platform := Actor{ContextType: models.ScopePlatform, PrincipalID: 41}
	provider, err := control.CreateProvider(ctx, platform, ProviderInput{Name: "discover", ScopeType: models.ScopePlatform, AdapterType: AdapterOpenAICompatible, Endpoint: upstream.URL + "/v1", AllowAllTenants: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := control.SetCredential(ctx, platform, provider.ID, "discovery-secret"); err != nil {
		t.Fatal(err)
	}

	response, err := NewRuntime(store, testEncryptionKey).DiscoverModels(ctx, platform, provider.ID)
	if err != nil {
		t.Fatal(err)
	}
	if response.ProviderConnectionID != provider.ID || len(response.Models) != 2 || response.Models[0].ID != "model-a" || response.Models[1].ID != "model-b" {
		t.Fatalf("unexpected discovery response: %+v", response)
	}
}

func TestDiscoverModelsRejectsNonOpenAIAdapter(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	control := NewControlPlane(store, testEncryptionKey)
	platform := Actor{ContextType: models.ScopePlatform, PrincipalID: 51}
	provider, err := control.CreateProvider(ctx, platform, ProviderInput{Name: "multimodal", ScopeType: models.ScopePlatform, AdapterType: AdapterDashScopeMultimodal, Endpoint: "https://example.test/embedding", AllowAllTenants: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewRuntime(store, testEncryptionKey).DiscoverModels(ctx, platform, provider.ID); err != ErrUnsupported {
		t.Fatalf("discover error = %v, want %v", err, ErrUnsupported)
	}
}

func TestInvalidStatusIsRejected(t *testing.T) {
	_, err := normalizeStatus("enabled")
	if err == nil {
		t.Fatal("unknown status must be rejected")
	}
}

func TestDeploymentChatMaxOutputTokensParameterIsExplicitAndValidated(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	control := NewControlPlane(store, testEncryptionKey)
	platform := Actor{ContextType: models.ScopePlatform, PrincipalID: 61}
	provider, err := control.CreateProvider(ctx, platform, ProviderInput{
		Name: "chat-provider", ScopeType: models.ScopePlatform, AdapterType: AdapterOpenAICompatible,
		Endpoint: "https://example.test/v1", AllowAllTenants: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	deployment, err := control.CreateDeployment(ctx, platform, DeploymentInput{
		ProviderConnectionID: provider.ID, Name: "chat", UpstreamModel: "model-a",
		Operations: []string{"chat"}, Modalities: []string{"text"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if deployment.ChatMaxOutputTokensParameter != ChatMaxOutputTokensParameterMaxTokens {
		t.Fatalf("default chat max output tokens parameter = %q", deployment.ChatMaxOutputTokensParameter)
	}

	_, err = control.UpdateDeployment(ctx, platform, deployment.ID, DeploymentInput{
		ProviderConnectionID: provider.ID, Name: "chat", UpstreamModel: "model-a",
		Operations: []string{"chat"}, Modalities: []string{"text"},
		ChatMaxOutputTokensParameter: "unsupported_parameter",
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid chat max output tokens parameter error = %v, want %v", err, ErrInvalidRequest)
	}

	_, err = control.UpdateDeployment(ctx, platform, deployment.ID, DeploymentInput{
		ProviderConnectionID: provider.ID, Name: "chat", UpstreamModel: "model-a",
		Operations: []string{"chat"}, Modalities: []string{"text"},
		ChatTemperatureMode: "unsupported_mode",
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid chat temperature mode error = %v, want %v", err, ErrInvalidRequest)
	}
}
