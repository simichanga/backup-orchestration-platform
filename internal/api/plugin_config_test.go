package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"bop/internal/inventory"
	"bop/internal/plugin/filesystem"
	"bop/internal/plugin/informix"
	"bop/internal/plugin/postgres"
)

func validInformixConfigBody() string {
	return `{
		"host": "10.0.0.5",
		"port": 9088,
		"username": "informix",
		"passwordEnv": "IFX_BACKUP_PASSWORD",
		"serverName": "informix",
		"database": "appdb",
		"tables": ["customers", "orders"],
		"agentPath": "/usr/local/bin/ifx-agent"
	}`
}

func validPostgresConfigBody() string {
	return `{
		"username": "backup_user",
		"passwordEnv": "PG_BACKUP_PASSWORD",
		"databases": ["appdb", "reporting"],
		"container": "my-postgres"
	}`
}

func validFilesystemConfigBody() string {
	return `{
		"paths": ["/var/lib/app", "/etc/app"],
		"excludes": ["*.tmp"]
	}`
}

func TestSetPluginConfigHandlerStoresValidConfig(t *testing.T) {
	t.Setenv("IFX_BACKUP_PASSWORD", "secret")
	md := openTestStore(t)

	req := httptest.NewRequest(http.MethodPut, "/v1/hosts/prod-informix/plugins/informix/config", strings.NewReader(validInformixConfigBody()))
	req.SetPathValue("host", "prod-informix")
	req.SetPathValue("plugin", "informix")
	rec := httptest.NewRecorder()
	setPluginConfigHandler(md)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	stored, found, err := md.GetPluginSettings(req.Context(), "prod-informix", "informix")
	if err != nil {
		t.Fatalf("GetPluginSettings: %v", err)
	}
	if !found {
		t.Fatalf("found = false, want true (PUT must persist the config)")
	}
	if !strings.Contains(stored, "10.0.0.5") {
		t.Errorf("stored config = %q, want it to contain the submitted host", stored)
	}
	// The password itself (resolved from password_env) must never be
	// persisted or echoed back - only the env var name.
	if strings.Contains(stored, "secret") {
		t.Errorf("stored config = %q, must not contain the resolved password", stored)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Errorf("response body = %q, must not contain the resolved password", rec.Body.String())
	}
}

func TestSetPluginConfigHandlerRejectsInvalidConfig(t *testing.T) {
	md := openTestStore(t)

	body := `{"host": "10.0.0.5"}` // missing everything else required
	req := httptest.NewRequest(http.MethodPut, "/v1/hosts/prod-informix/plugins/informix/config", strings.NewReader(body))
	req.SetPathValue("host", "prod-informix")
	req.SetPathValue("plugin", "informix")
	rec := httptest.NewRecorder()
	setPluginConfigHandler(md)(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}

	if _, found, err := md.GetPluginSettings(req.Context(), "prod-informix", "informix"); err != nil || found {
		t.Errorf("found=%v err=%v, want nothing persisted for an invalid config", found, err)
	}
}

func TestSetPluginConfigHandlerRejectsUnsupportedPlugin(t *testing.T) {
	md := openTestStore(t)

	// shellcmd is a shared command-quoting helper, not a registered
	// BackupPlugin - it has no ParseConfig and never will.
	req := httptest.NewRequest(http.MethodPut, "/v1/hosts/prod-db/plugins/shellcmd/config", strings.NewReader(`{}`))
	req.SetPathValue("host", "prod-db")
	req.SetPathValue("plugin", "shellcmd")
	rec := httptest.NewRecorder()
	setPluginConfigHandler(md)(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (shellcmd has no web configuration)", rec.Code)
	}
}

func TestSetPluginConfigHandlerRejectsMalformedJSON(t *testing.T) {
	md := openTestStore(t)

	req := httptest.NewRequest(http.MethodPut, "/v1/hosts/prod-informix/plugins/informix/config", bytes.NewReader([]byte("not json")))
	req.SetPathValue("host", "prod-informix")
	req.SetPathValue("plugin", "informix")
	rec := httptest.NewRecorder()
	setPluginConfigHandler(md)(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestGetPluginConfigHandlerReturnsStoredConfig(t *testing.T) {
	t.Setenv("IFX_BACKUP_PASSWORD", "secret")
	md := openTestStore(t)

	putReq := httptest.NewRequest(http.MethodPut, "/v1/hosts/prod-informix/plugins/informix/config", strings.NewReader(validInformixConfigBody()))
	putReq.SetPathValue("host", "prod-informix")
	putReq.SetPathValue("plugin", "informix")
	setPluginConfigHandler(md)(httptest.NewRecorder(), putReq)

	getReq := httptest.NewRequest(http.MethodGet, "/v1/hosts/prod-informix/plugins/informix/config", nil)
	getReq.SetPathValue("host", "prod-informix")
	getReq.SetPathValue("plugin", "informix")
	rec := httptest.NewRecorder()
	getPluginConfigHandler(md)(rec, getReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var got informixConfigDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got.Host != "10.0.0.5" || got.Database != "appdb" || len(got.Tables) != 2 {
		t.Errorf("got = %+v, want the stored config echoed back", got)
	}
}

// TestStoredConfigIsDirectlyConsumableByParseConfig guards the exact bug
// this handler had during development: setPluginConfigHandler validated a
// snake_case-translated map but then persisted the camelCase DTO's own
// JSON verbatim, so Controller.resolvePluginConfig's generic pass-through
// (which hands stored JSON straight to a plugin factory, unmodified - see
// controller.go) fed informix.ParseConfig a shape it couldn't read,
// failing with "config.password_env is required" even though the caller
// had just supplied it. Caught by a live end-to-end run, not by any
// handler-level test before this one existed - this test is what should
// catch a regression next time, without needing a real controller/job.
func TestStoredConfigIsDirectlyConsumableByParseConfig(t *testing.T) {
	t.Setenv("IFX_BACKUP_PASSWORD", "secret")
	md := openTestStore(t)

	putReq := httptest.NewRequest(http.MethodPut, "/v1/hosts/prod-informix/plugins/informix/config", strings.NewReader(validInformixConfigBody()))
	putReq.SetPathValue("host", "prod-informix")
	putReq.SetPathValue("plugin", "informix")
	setPluginConfigHandler(md)(httptest.NewRecorder(), putReq)

	stored, found, err := md.GetPluginSettings(putReq.Context(), "prod-informix", "informix")
	if err != nil || !found {
		t.Fatalf("GetPluginSettings: found=%v err=%v", found, err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(stored), &raw); err != nil {
		t.Fatalf("stored config is not valid JSON: %v", err)
	}
	// This is exactly what Controller.resolvePluginConfig does with a
	// stored override - no translation, straight to the factory's
	// ParseConfig call.
	if _, err := informix.ParseConfig(&inventory.PluginConfig{Config: raw}); err != nil {
		t.Fatalf("stored config is not directly consumable by informix.ParseConfig: %v", err)
	}
}

func TestGetPluginConfigHandlerNotFound(t *testing.T) {
	md := openTestStore(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/hosts/no-such-host/plugins/informix/config", nil)
	req.SetPathValue("host", "no-such-host")
	req.SetPathValue("plugin", "informix")
	rec := httptest.NewRecorder()
	getPluginConfigHandler(md)(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestGetPluginConfigHandlerRejectsUnsupportedPlugin guards the bug fixed
// alongside postgres/filesystem web configuration: the GET handler used to
// translate every stored config through informixDTOFromRawConfig
// unconditionally, which was harmless only because nothing but informix
// could ever be stored. Once other plugins can be stored, GET needs the
// same per-plugin dispatch PUT already had.
func TestGetPluginConfigHandlerRejectsUnsupportedPlugin(t *testing.T) {
	md := openTestStore(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/hosts/prod-db/plugins/shellcmd/config", nil)
	req.SetPathValue("host", "prod-db")
	req.SetPathValue("plugin", "shellcmd")
	rec := httptest.NewRecorder()
	getPluginConfigHandler(md)(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (shellcmd has no web configuration)", rec.Code)
	}
}

func TestSetPluginConfigHandlerStoresValidPostgresConfig(t *testing.T) {
	t.Setenv("PG_BACKUP_PASSWORD", "secret")
	md := openTestStore(t)

	req := httptest.NewRequest(http.MethodPut, "/v1/hosts/prod-db/plugins/postgres/config", strings.NewReader(validPostgresConfigBody()))
	req.SetPathValue("host", "prod-db")
	req.SetPathValue("plugin", "postgres")
	rec := httptest.NewRecorder()
	setPluginConfigHandler(md)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	stored, found, err := md.GetPluginSettings(req.Context(), "prod-db", "postgres")
	if err != nil {
		t.Fatalf("GetPluginSettings: %v", err)
	}
	if !found {
		t.Fatalf("found = false, want true (PUT must persist the config)")
	}
	if !strings.Contains(stored, "appdb") {
		t.Errorf("stored config = %q, want it to contain the submitted database", stored)
	}
	if strings.Contains(stored, "secret") || strings.Contains(rec.Body.String(), "secret") {
		t.Errorf("stored/response config must not contain the resolved password")
	}
}

func TestSetPluginConfigHandlerRejectsInvalidPostgresConfig(t *testing.T) {
	md := openTestStore(t)

	body := `{"username": "backup_user"}` // missing passwordEnv and databases
	req := httptest.NewRequest(http.MethodPut, "/v1/hosts/prod-db/plugins/postgres/config", strings.NewReader(body))
	req.SetPathValue("host", "prod-db")
	req.SetPathValue("plugin", "postgres")
	rec := httptest.NewRecorder()
	setPluginConfigHandler(md)(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
	if _, found, err := md.GetPluginSettings(req.Context(), "prod-db", "postgres"); err != nil || found {
		t.Errorf("found=%v err=%v, want nothing persisted for an invalid config", found, err)
	}
}

func TestGetPluginConfigHandlerReturnsStoredPostgresConfig(t *testing.T) {
	t.Setenv("PG_BACKUP_PASSWORD", "secret")
	md := openTestStore(t)

	putReq := httptest.NewRequest(http.MethodPut, "/v1/hosts/prod-db/plugins/postgres/config", strings.NewReader(validPostgresConfigBody()))
	putReq.SetPathValue("host", "prod-db")
	putReq.SetPathValue("plugin", "postgres")
	setPluginConfigHandler(md)(httptest.NewRecorder(), putReq)

	getReq := httptest.NewRequest(http.MethodGet, "/v1/hosts/prod-db/plugins/postgres/config", nil)
	getReq.SetPathValue("host", "prod-db")
	getReq.SetPathValue("plugin", "postgres")
	rec := httptest.NewRecorder()
	getPluginConfigHandler(md)(rec, getReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var got postgresConfigDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got.Username != "backup_user" || len(got.Databases) != 2 || got.Container != "my-postgres" {
		t.Errorf("got = %+v, want the stored config echoed back", got)
	}
}

// TestStoredPostgresConfigIsDirectlyConsumableByParseConfig is postgres's
// equivalent of TestStoredConfigIsDirectlyConsumableByParseConfig above -
// see that test's comment for why this is worth guarding at the handler
// level rather than trusting a live run to catch it.
func TestStoredPostgresConfigIsDirectlyConsumableByParseConfig(t *testing.T) {
	t.Setenv("PG_BACKUP_PASSWORD", "secret")
	md := openTestStore(t)

	putReq := httptest.NewRequest(http.MethodPut, "/v1/hosts/prod-db/plugins/postgres/config", strings.NewReader(validPostgresConfigBody()))
	putReq.SetPathValue("host", "prod-db")
	putReq.SetPathValue("plugin", "postgres")
	setPluginConfigHandler(md)(httptest.NewRecorder(), putReq)

	stored, found, err := md.GetPluginSettings(putReq.Context(), "prod-db", "postgres")
	if err != nil || !found {
		t.Fatalf("GetPluginSettings: found=%v err=%v", found, err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(stored), &raw); err != nil {
		t.Fatalf("stored config is not valid JSON: %v", err)
	}
	if _, err := postgres.ParseConfig(&inventory.PluginConfig{Config: raw}); err != nil {
		t.Fatalf("stored config is not directly consumable by postgres.ParseConfig: %v", err)
	}
}

func TestSetPluginConfigHandlerStoresValidFilesystemConfig(t *testing.T) {
	md := openTestStore(t)

	req := httptest.NewRequest(http.MethodPut, "/v1/hosts/prod-fs/plugins/filesystem/config", strings.NewReader(validFilesystemConfigBody()))
	req.SetPathValue("host", "prod-fs")
	req.SetPathValue("plugin", "filesystem")
	rec := httptest.NewRecorder()
	setPluginConfigHandler(md)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	stored, found, err := md.GetPluginSettings(req.Context(), "prod-fs", "filesystem")
	if err != nil {
		t.Fatalf("GetPluginSettings: %v", err)
	}
	if !found {
		t.Fatalf("found = false, want true (PUT must persist the config)")
	}
	if !strings.Contains(stored, "/var/lib/app") {
		t.Errorf("stored config = %q, want it to contain the submitted path", stored)
	}
}

func TestSetPluginConfigHandlerRejectsInvalidFilesystemConfig(t *testing.T) {
	md := openTestStore(t)

	body := `{"paths": ["relative/path"]}` // not absolute
	req := httptest.NewRequest(http.MethodPut, "/v1/hosts/prod-fs/plugins/filesystem/config", strings.NewReader(body))
	req.SetPathValue("host", "prod-fs")
	req.SetPathValue("plugin", "filesystem")
	rec := httptest.NewRecorder()
	setPluginConfigHandler(md)(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
	if _, found, err := md.GetPluginSettings(req.Context(), "prod-fs", "filesystem"); err != nil || found {
		t.Errorf("found=%v err=%v, want nothing persisted for an invalid config", found, err)
	}
}

func TestGetPluginConfigHandlerReturnsStoredFilesystemConfig(t *testing.T) {
	md := openTestStore(t)

	putReq := httptest.NewRequest(http.MethodPut, "/v1/hosts/prod-fs/plugins/filesystem/config", strings.NewReader(validFilesystemConfigBody()))
	putReq.SetPathValue("host", "prod-fs")
	putReq.SetPathValue("plugin", "filesystem")
	setPluginConfigHandler(md)(httptest.NewRecorder(), putReq)

	getReq := httptest.NewRequest(http.MethodGet, "/v1/hosts/prod-fs/plugins/filesystem/config", nil)
	getReq.SetPathValue("host", "prod-fs")
	getReq.SetPathValue("plugin", "filesystem")
	rec := httptest.NewRecorder()
	getPluginConfigHandler(md)(rec, getReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var got filesystemConfigDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(got.Paths) != 2 || len(got.Excludes) != 1 {
		t.Errorf("got = %+v, want the stored config echoed back", got)
	}
}

// TestStoredFilesystemConfigIsDirectlyConsumableByParseConfig is
// filesystem's equivalent of TestStoredConfigIsDirectlyConsumableByParseConfig
// above - see that test's comment for why this is worth guarding at the
// handler level rather than trusting a live run to catch it.
func TestStoredFilesystemConfigIsDirectlyConsumableByParseConfig(t *testing.T) {
	md := openTestStore(t)

	putReq := httptest.NewRequest(http.MethodPut, "/v1/hosts/prod-fs/plugins/filesystem/config", strings.NewReader(validFilesystemConfigBody()))
	putReq.SetPathValue("host", "prod-fs")
	putReq.SetPathValue("plugin", "filesystem")
	setPluginConfigHandler(md)(httptest.NewRecorder(), putReq)

	stored, found, err := md.GetPluginSettings(putReq.Context(), "prod-fs", "filesystem")
	if err != nil || !found {
		t.Fatalf("GetPluginSettings: found=%v err=%v", found, err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(stored), &raw); err != nil {
		t.Fatalf("stored config is not valid JSON: %v", err)
	}
	if _, err := filesystem.ParseConfig(&inventory.PluginConfig{Config: raw}); err != nil {
		t.Fatalf("stored config is not directly consumable by filesystem.ParseConfig: %v", err)
	}
}
