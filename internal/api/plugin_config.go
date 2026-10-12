package api

import (
	"encoding/json"
	"io"
	"net/http"

	"bop/internal/inventory"
	"bop/internal/metadata"
	"bop/internal/plugin/filesystem"
	"bop/internal/plugin/informix"
	"bop/internal/plugin/postgres"
)

// pluginConfigDTO is the wire shape for one plugin's web-configured
// settings - camelCase JSON tags, matching every other DTO in this
// package. decode reads a PUT body into the DTO; toRawConfig/fromRawConfig
// translate to and from the generic map[string]interface{} shape each
// plugin's ParseConfig (and inventory.yaml's config: block) expects -
// snake_case keys. Controller.resolvePluginConfig is generic across
// plugins and feeds whatever's stored straight to that plugin's
// ParseConfig, so this indirection exists once per plugin rather than
// assuming the camelCase wire format doubles as the storage format.
type pluginConfigDTO interface {
	decode(body []byte) error
	toRawConfig() map[string]interface{}
	fromRawConfig(raw map[string]interface{})
	validate() error
}

// pluginConfigDTOs maps a plugin name to a constructor for its DTO. Web
// configuration is wired up for informix, postgres, and filesystem - any
// other plugin name (including shellcmd, which is a shared command-quoting
// helper, not a registered BackupPlugin) falls through to the "not
// available" branch in both handlers below.
var pluginConfigDTOs = map[string]func() pluginConfigDTO{
	"informix":   func() pluginConfigDTO { return &informixConfigDTO{} },
	"postgres":   func() pluginConfigDTO { return &postgresConfigDTO{} },
	"filesystem": func() pluginConfigDTO { return &filesystemConfigDTO{} },
}

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

func (dto *informixConfigDTO) decode(body []byte) error { return json.Unmarshal(body, dto) }

func (dto *informixConfigDTO) toRawConfig() map[string]interface{} {
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

func (dto *informixConfigDTO) fromRawConfig(raw map[string]interface{}) {
	dto.Host, _ = raw["host"].(string)
	if port, ok := raw["port"].(float64); ok {
		dto.Port = int(port)
	}
	dto.Username, _ = raw["username"].(string)
	dto.PasswordEnv, _ = raw["password_env"].(string)
	dto.ServerName, _ = raw["server_name"].(string)
	dto.Database, _ = raw["database"].(string)
	dto.AgentPath, _ = raw["agent_path"].(string)
	dto.Tables = stringSliceFromRaw(raw["tables"])
}

// validate runs the exact same validation the inventory.yaml path goes
// through, so a bad config fails here with the same clear error it would
// at plugin-instantiation time, not silently stored and discovered broken
// on the next job.
func (dto *informixConfigDTO) validate() error {
	_, err := informix.ParseConfig(&inventory.PluginConfig{Config: dto.toRawConfig()})
	return err
}

// postgresConfigDTO has no password field by design - only PasswordEnv (a
// variable *name*, not a secret), the same convention postgres.Config and
// inventory.yaml's config.password_env already follow, so nothing here
// ever needs redacting before being echoed back by GET.
type postgresConfigDTO struct {
	Username    string   `json:"username"`
	PasswordEnv string   `json:"passwordEnv"`
	Databases   []string `json:"databases"`
	Container   string   `json:"container"`
}

func (dto *postgresConfigDTO) decode(body []byte) error { return json.Unmarshal(body, dto) }

func (dto *postgresConfigDTO) toRawConfig() map[string]interface{} {
	databases := make([]interface{}, len(dto.Databases))
	for i, d := range dto.Databases {
		databases[i] = d
	}
	return map[string]interface{}{
		"username":     dto.Username,
		"password_env": dto.PasswordEnv,
		"databases":    databases,
		"container":    dto.Container,
	}
}

func (dto *postgresConfigDTO) fromRawConfig(raw map[string]interface{}) {
	dto.Username, _ = raw["username"].(string)
	dto.PasswordEnv, _ = raw["password_env"].(string)
	dto.Container, _ = raw["container"].(string)
	dto.Databases = stringSliceFromRaw(raw["databases"])
}

func (dto *postgresConfigDTO) validate() error {
	_, err := postgres.ParseConfig(&inventory.PluginConfig{Config: dto.toRawConfig()})
	return err
}

type filesystemConfigDTO struct {
	Paths    []string `json:"paths"`
	Excludes []string `json:"excludes"`
}

func (dto *filesystemConfigDTO) decode(body []byte) error { return json.Unmarshal(body, dto) }

func (dto *filesystemConfigDTO) toRawConfig() map[string]interface{} {
	paths := make([]interface{}, len(dto.Paths))
	for i, p := range dto.Paths {
		paths[i] = p
	}
	excludes := make([]interface{}, len(dto.Excludes))
	for i, e := range dto.Excludes {
		excludes[i] = e
	}
	return map[string]interface{}{
		"paths":    paths,
		"excludes": excludes,
	}
}

func (dto *filesystemConfigDTO) fromRawConfig(raw map[string]interface{}) {
	dto.Paths = stringSliceFromRaw(raw["paths"])
	dto.Excludes = stringSliceFromRaw(raw["excludes"])
}

func (dto *filesystemConfigDTO) validate() error {
	_, err := filesystem.ParseConfig(&inventory.PluginConfig{Config: dto.toRawConfig()})
	return err
}

func stringSliceFromRaw(v interface{}) []string {
	raw, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, len(raw))
	for i, item := range raw {
		out[i], _ = item.(string)
	}
	return out
}

// getPluginConfigHandler serves GET /v1/hosts/{host}/plugins/{plugin}/config.
// The stored JSON is in the generic snake_case shape each plugin's
// ParseConfig expects - translated back to that plugin's camelCase DTO
// before being written out, so the web UI always sees the same wire shape
// it PUTs.
func getPluginConfigHandler(md *metadata.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host, pluginName := r.PathValue("host"), r.PathValue("plugin")
		newDTO, ok := pluginConfigDTOs[pluginName]
		if !ok {
			writeError(w, http.StatusBadRequest, "web configuration isn't available for \""+pluginName+"\"")
			return
		}

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
		dto := newDTO()
		dto.fromRawConfig(raw)
		writeJSON(w, http.StatusOK, dto)
	}
}

// setPluginConfigHandler serves PUT /v1/hosts/{host}/plugins/{plugin}/config.
// Stores the translated, re-marshaled rawConfig map, not the raw request
// body, so an unexpected extra field (e.g. a client mistakenly sending a
// plaintext "password") can never end up persisted - and so that
// Controller.resolvePluginConfig's generic, plugin-agnostic pass-through
// to the plugin factory gets exactly the shape ParseConfig expects.
func setPluginConfigHandler(md *metadata.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host, pluginName := r.PathValue("host"), r.PathValue("plugin")
		newDTO, ok := pluginConfigDTOs[pluginName]
		if !ok {
			writeError(w, http.StatusBadRequest, "web configuration isn't available for \""+pluginName+"\"")
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		dto := newDTO()
		if err := dto.decode(body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}

		if err := dto.validate(); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		configJSON, err := json.Marshal(dto.toRawConfig())
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
