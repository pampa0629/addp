"""Exercise observer rejection against actual local HTTP; no product acceptance claim."""
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from importlib import import_module
import os
import json
from pathlib import Path
import subprocess
import tempfile
import threading
import unittest
from unittest.mock import patch

scenario = import_module("scripts.test.develop-notebook-storage-online")


class NotebookObserverTest(unittest.TestCase):
    def setUp(self):
        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                if self.path == "/api/v1/system/auth/context":
                    self.server.identity_reads += 1
                    self.send_response(200)
                    self.end_headers()
                    self.wfile.write(json.dumps(self.server.identity).encode())
                    return
                cookie = self.headers.get("Cookie", "")
                if self.path == "/open":
                    self.send_response(200)
                    self.send_header("Set-Cookie", "addp_notebook_session=private; Path=/owned/")
                    self.end_headers()
                    self.wfile.write(b'{}')
                elif self.path == "/owned/bytes" and "addp_notebook_session=private" in cookie:
                    self.send_response(200)
                    self.end_headers()
                    self.wfile.write(b'\x00notebook\xff')
                else:
                    self.send_response(401)
                    self.end_headers()

            def log_message(self, *args):
                pass

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.server.identity_reads = 0
        self.server.identity = {
            "principal": {"type": "user"}, "context": {"type": "tenant", "tenant_id": "7"},
            "token": {"type": "first_party_access_token"},
            "authorization": {"role_assignments": [
                {"role_key": "tenant.engine_management_reader", "permissions": ["system.engine.read"]},
                {"role_key": "online.notebook", "permissions": sorted(scenario.PERMISSIONS - {"system.engine.read"})},
            ]},
        }
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.environment = patch.dict(os.environ, {"GATEWAY_URL": "http://127.0.0.1:" + str(self.server.server_port)})
        self.environment.start()
        self.addCleanup(self.environment.stop)
        self.addCleanup(self.server.server_close)
        self.addCleanup(self.server.shutdown)

    def test_exact_permissions_are_checked_from_one_auth_context_request(self):
        identity = scenario.validate_notebook_identity("ordinary-user", 7)
        self.assertIn("tenant.engine_management_reader", identity["roles"])
        self.assertEqual(self.server.identity_reads, 1)

    def test_extra_permission_cannot_pass(self):
        self.server.identity["authorization"]["role_assignments"][1]["permissions"].append("system.engine.create")
        with self.assertRaises(scenario.api.SuiteError):
            scenario.validate_notebook_identity("extra-permission", 7)

    def test_unpublished_role_cannot_replace_builtin_reader(self):
        self.server.identity["authorization"]["role_assignments"][0]["role_key"] = "online.unpublished_reader"
        with self.assertRaises(scenario.api.SuiteError):
            scenario.validate_notebook_identity("wrong-role", 7)

    def test_cookie_scope_and_binary_bytes_are_preserved(self):
        client = scenario.Client("ordinary-user")
        client.request("GET", "/open", (200,))
        self.assertEqual(client.request("GET", "/owned/bytes", (200,), raw=True), b'\x00notebook\xff')
        self.assertEqual(client.cookie_header("/other/bytes"), "")
        self.assertNotEqual(client.cookie_header("/owned/bytes"), "")

    def test_wrong_identity_status_cannot_pass(self):
        client = scenario.Client("other-user")
        with self.assertRaises(scenario.api.SuiteError):
            client.request("GET", "/owned/bytes", (200,), raw=True)
        self.assertEqual(client.request("GET", "/owned/bytes", (401,)), {})

    def test_same_size_corruption_cannot_pass(self):
        with self.assertRaises(scenario.api.SuiteError):
            scenario.check_bytes(b"abc", b"abd", "cross-language")
        self.assertEqual(scenario.check_bytes(b"abc", b"abc", "cross-language")["bytes"], 3)

    def test_reachable_workspace_never_counts_as_cleaned(self):
        with patch.object(scenario, "session_resources", return_value=[]), patch.object(scenario.subprocess, "run") as run:
            identity = {"session_id": "fixture"}
            run.return_value.stderr = b""
            run.return_value.returncode = 1
            self.assertFalse(scenario.process_gone(identity))
            run.return_value.returncode = 0
            self.assertTrue(scenario.process_gone(identity))
            run.return_value.returncode = 125
            with self.assertRaises(scenario.api.SuiteError):
                scenario.process_gone(identity)


    def test_hosted_registration_pins_product_and_preserves_standard_lifecycle(self):
        root = Path(__file__).resolve().parents[2]
        script = (root / "scripts/test/online-hosted-notebook-storage-gate.sh").read_text()
        for fragment in ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"',
                         'git -C "$ROOT_DIR" rev-parse HEAD', 'JUPYTER_SESSION_TTL_SECONDS=300',
                         'bash scripts/dev/start.sh -jupyter', 'bash scripts/dev/start.sh -develop',
                         'develop=notebook-http-documents', 'make -C "$HARNESS" test-online'):
            self.assertIn(fragment, script)
        self.assertNotIn("keepalive.sh", script)
        self.assertNotIn("--exact-process -jupyter", script)


class NotebookFixtureCompileTest(unittest.TestCase):
    def test_identity_patch_compiles_without_modifying_product_checkout(self):
        root = Path(__file__).resolve().parents[2]
        source = Path(os.environ.get("ADDP_NOTEBOOK_IDENTITY_SOURCE_ROOT", root)).resolve()
        with tempfile.TemporaryDirectory(prefix="addp-notebook-identity-check-") as directory:
            checkout = Path(directory) / "identity"
            subprocess.run(["git", "clone", "--shared", "--no-checkout", str(source), str(checkout)], check=True, capture_output=True)
            subprocess.run(["git", "checkout", "--detach", "HEAD"], cwd=checkout, check=True, capture_output=True)
            subprocess.run(["git", "apply", str(root / "scripts/test/develop-notebook-identity.patch")], cwd=checkout, check=True, capture_output=True)
            # Resolve the complete catalog from one source, including before publication.
            test_file = checkout / "system/backend/cmd/online-test-fixture/notebook_storage_fixture_test.go"
            test_file.write_text('''package main
import ("testing"; "reflect"; "slices"; "os"; "net/url"; "strings"; "time"; "context";
 "github.com/addp/common/authorization"; "github.com/addp/system/internal/config";
 "github.com/addp/system/internal/migration"; "github.com/addp/system/internal/testsupport";
 "github.com/addp/system/internal/models"; "gorm.io/driver/postgres"; "gorm.io/gorm"; "gorm.io/gorm/logger")
func TestNotebookStoragePermissionBoundary(t *testing.T) {
    got, err := suitePermissions("develop-notebook-storage")
    expected := []string{"develop.notebook.create", "develop.notebook.read", "develop.notebook.update", "develop.notebook.delete", "develop.task.read"}
    if err != nil || !reflect.DeepEqual(got,expected) { t.Fatalf("unexpected ordinary-user permissions: %v %v",got,err) }
    catalog, err := authorization.LoadRepositoryAuthorizationCatalog(os.Getenv("ADDP_NOTEBOOK_CATALOG_SOURCE_ROOT"))
    if err != nil { t.Fatal(err) }
    for _, key := range got {
        found := false
        for _, permission := range catalog.Permissions {
            if permission.Key == key { found = permission.Status == "active" && permission.TenantCustomizable && slices.Contains(permission.AllowedScopeTypes,"tenant") }
        }
        if !found { t.Fatalf("custom permission cannot be assigned: %s",key) }
    }
    found := false
    for _, role := range catalog.Roles {
        if role.Key != "tenant.engine_management_reader" { continue }
        found = role.RoleType == "tenant_builtin" && slices.Equal(role.Permissions,[]string{"system.engine.read"}) && slices.Equal(role.AllowedScopeTypes,[]string{"tenant"}) && slices.Equal(role.AllowedPrincipalTypes,[]string{"user"})
    }
    if !found { t.Fatal("published minimum Tenant User reader missing") }
    for _, permission := range catalog.Permissions {
        if permission.Key == "system.engine.read" && permission.TenantCustomizable { t.Fatal("engine management read became customizable") }
    }
    if len(suiteSourceInitializerPermissions("develop-notebook-storage")) != 0 { t.Fatal("Notebook fixture expanded source initializer permissions") }
}
func TestNotebookStorageEngineFixtureAgainstPostgres(t *testing.T) {
    dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
    if dsn == "" { t.Skip("requires standard System IAM PostgreSQL gate") }
    testsupport.RequireDisposablePostgresDSN(t,dsn)
    db,err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger:logger.Default.LogMode(logger.Silent)})
    if err != nil { t.Fatal(err) }
    connection,err := db.DB()
    if err != nil { t.Fatal(err) }
    reset := func() {
        if err := db.Exec("DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE").Error; err != nil { t.Error(err) }
    }
    t.Cleanup(func(){ reset(); _ = connection.Close() })
    reset()
    ctx,cancel := context.WithTimeout(t.Context(),90*time.Second)
    defer cancel()
    if err := migration.NewRunner(dsn).Run(ctx); err != nil { t.Fatal(err) }
    address,err := url.Parse(dsn)
    if err != nil { t.Fatal(err) }
    password,_ := address.User.Password()
    cfg := &config.Config{PostgresHost:address.Hostname(),PostgresPort:address.Port(),PostgresUser:address.User.Username(),
        PostgresPassword:password,PostgresDB:strings.TrimPrefix(address.Path,"/"),EncryptionKey:[]byte("0123456789abcdef0123456789abcdef")}
    if cfg.PostgresPort == "" { cfg.PostgresPort = "5432" }
    var engineID uint
    for range 2 {
        engine,err := createNotebookFixtureEngine(cfg)
        if err != nil { t.Fatal(err) }
        if engine.ID == 0 || (engineID != 0 && engine.ID != engineID) { t.Fatal("fixture lost stable real Engine identity") }
        engineID = engine.ID
        var stored models.Engine
        if err := db.Table("system.engines").First(&stored,engine.ID).Error; err != nil { t.Fatal(err) }
        if stored.EngineType != "jupyter" || !stored.IsBuiltin || stored.TenantID != nil { t.Fatal("fixture Engine declaration changed") }
    }
}
''')
            pattern = "^TestNotebookStoragePermissionBoundary$"
            postgres = os.environ.get("ADDP_NOTEBOOK_IDENTITY_POSTGRES") == "1"
            if postgres:
                pattern = "^TestNotebookStorage(PermissionBoundary|EngineFixtureAgainstPostgres)$"
            env = {**os.environ, "GOWORK": "off", "ADDP_NOTEBOOK_CATALOG_SOURCE_ROOT": str(source)}
            result = subprocess.run(["go", "test", "./cmd/online-test-fixture", "-run", pattern, "-count=1", "-v"],
                                    cwd=checkout / "system/backend", env=env, capture_output=True, text=True, timeout=240)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            if postgres:
                print("Notebook identity product base:", subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=checkout, text=True).strip())
                self.assertIn("--- PASS: TestNotebookStorageEngineFixtureAgainstPostgres ", result.stdout)
                self.assertNotIn("--- SKIP:", result.stdout)
                print(result.stdout)


if __name__ == "__main__":
    unittest.main()
