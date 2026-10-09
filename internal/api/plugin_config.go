package api

import (
	"encoding/json"
	"net/http"

	"bop/internal/inventory"
	"bop/internal/metadata"
	"bop/internal/plugin/informix"
)

// informixConfigDTO is the wire shape for the informix plugin's
// web-configured settings. camelCase JSON tags, matching every other DTO
// in this package (jobSummary, snapshotSummary, ...). This is NOT the
// shape persisted in the metadata store or handed to the plugin factory -
// see toRawConfig/dtoFromRawConfig: Controller.resolvePluginConfig is
// generic across plugins and feeds whatever's stored straight to
// informix.ParseConfig, which (like every plugin's config parser) expects
// the same snake_case keys inventory.yaml's config: block uses
// (password_env, server_name, ...) - so this handler translates in both
// directions rather than assuming its own camelCase wire format doubles
// as the storage format.
//
// Deliberately has no password field - only PasswordEnv (a variable
// *name*, not a secret) - the same convention informix.Config and
// inventory.yaml's config.password_env already follow, so nothing here
// ever needs redacting before being echoed back by GET.
//
// Web configuration is only wired up for "informix" today - the route is
// generically named (/v1/hosts/{host}/plugins/{plugin}/config) for future
// plugins, but only one validator/translator exists so far;
// setPluginConfigHandler rejects any other plugin name explicitly rather
// than accepting and silently mis-validating it.
type informixConfigDTO struct {
	Host        string   `json:"host"`
	Port        int      `json:"port"`
	Username    string   `json:"username"`
	PasswordEnv string   `json:"passwordEnv"`
	ServerName  string   `json:"serverName"`
	Database    string   `json:"database"`
	Tables      []string `json:"tables"`
	AgentPath   string   `json:"agentPath"`
}

// toRawConfig builds the generic map[string]interface{} shape
// informix.ParseConfig (and inventory.yaml's config: block) expects -
// snake_case keys, tables as []interface{}.
func (dto informixConfigDTO) toRawConfig() map[string]interface{} {
	tables := make([]interface{}, len(dto.Tables))
	for i, t := range dto.Tables {
		tables[i] = t
	}
	return map[string]interface{}{
		"host":         dto.Host,
		"port":         dto.Port,
		"username":     dto.Username,
		"password_env": dto.PasswordEnv,
		"server_name":  dto.ServerName,
		"database":     dto.Database,
		"tables":       tables,
		"agent_path":   dto.AgentPath,
	}
}

// informixDTOFromRawConfig is toRawConfig's inverse, used by
// getPluginConfigHandler to translate the stored snake_case config back
// into this handler's camelCase wire format.
func informixDTOFromRawConfig(raw map[string]interface{}) informixConfigDTO {
	dto := informixConfigDTO{}
	dto.Host, _ = raw["host"].(string)
	if port, ok := raw["port"].(float64); ok {
		dto.Port = int(port)
	}
	dto.Username, _ = raw["username"].(string)
	dto.PasswordEnv, _ = raw["password_env"].(string)
	dto.ServerName, _ = raw["server_name"].(string)
	dto.Database, _ = raw["database"].(string)
	dto.AgentPath, _ = raw["agent_path"].(string)
	if rawTables, ok := raw["tables"].([]interface{}); ok {
		dto.Tables = make([]string, len(rawTables))
		for i, t := range rawTables {
			dto.Tables[i], _ = t.(string)
		}
	}
	return dto
}

// getPluginConfigHandler serves GET /v1/hosts/{host}/plugins/{plugin}/config.
// The stored JSON is in the generic snake_case shape (see toRawConfig) -
// translated back to the camelCase DTO before being written out, so the
// web UI always sees the same wire shape it PUTs.
func getPluginConfigHandler(md *metadata.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host, pluginName := r.PathValue("host"), r.PathValue("plugin")
		configJSON, found, err := md.GetPluginSettings(r.Context(), host, pluginName)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !found {
			writeError(w, http.StatusNotFound, "no stored config for this host/plugin")
			return
		}
		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(configJSON), &raw); err != nil {
			writeError(w, http.StatusInternalServerError, "stored config is not valid JSON: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, informixDTOFromRawConfig(raw))
	}
}

// setPluginConfigHandler serves PUT /v1/hosts/{host}/plugins/{plugin}/config.
// Validates the submitted config through informix.ParseConfig - the exact
// same validation the inventory.yaml path goes through, so a bad config
// fails here with the same clear error it would at plugin-instantiation
// time, not silently stored and discovered broken on the next job. Stores
// the translated, re-marshaled rawConfig map, not the raw request body, so
// an unexpected extra field (e.g. a client mistakenly sending a plaintext
// "password") can never end up persisted - and so that
// Controller.resolvePluginConfig's generic, plugin-agnostic pass-through
// to the plugin factory gets exactly the shape ParseConfig expects.
func setPluginConfigHandler(md *metadata.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host, pluginName := r.PathValue("host"), r.PathValue("plugin")
		if pluginName != "informix" {
			writeError(w, http.StatusBadRequest, "web configuration is only supported for the informix plugin today")
			return
		}

		var dto informixConfigDTO
		if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}

		rawConfig := dto.toRawConfig()
		if _, err := informix.ParseConfig(&inventory.PluginConfig{Config: rawConfig}); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		configJSON, err := json.Marshal(rawConfig)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "encode config: "+err.Error())
			return
		}
		if err := md.SetPluginSettings(r.Context(), host, pluginName, string(configJSON)); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, dto)
	}
}
