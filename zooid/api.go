package zooid

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"fiatjaf.com/nostr"
	"github.com/BurntSushi/toml"
)

// APIHandler handles REST API requests for managing virtual relays
type APIHandler struct {
	whitelist map[string]bool
	configDir string
}

// NewAPIHandler creates a new API handler with the given whitelist
func NewAPIHandler(whitelist string, configDir string) *APIHandler {
	w := make(map[string]bool)
	for _, pubkey := range Split(whitelist, ",") {
		pubkey = strings.TrimSpace(pubkey)
		if pubkey != "" {
			w[pubkey] = true
		}
	}
	return &APIHandler{
		whitelist: w,
		configDir: configDir,
	}
}

// ServeHTTP implements the http.Handler interface
func (api *APIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Authenticate the request using NIP-98
	pubkey, err := validateNIP98Auth(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	// Check if pubkey is in whitelist
	if !api.whitelist[pubkey.Hex()] {
		writeError(w, http.StatusForbidden, "pubkey not in whitelist")
		return
	}

	// Route the request
	path := strings.TrimPrefix(r.URL.Path, "/")
	parts := strings.Split(path, "/")

	if len(parts) < 2 || parts[0] != "relay" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	id := parts[1]
	if id == "" {
		writeError(w, http.StatusBadRequest, "relay id is required")
		return
	}

	switch r.Method {
	case http.MethodPost:
		api.createRelay(w, r, id)
	case http.MethodPut:
		api.updateRelay(w, r, id)
	case http.MethodPatch:
		api.patchRelay(w, r, id)
	case http.MethodDelete:
		api.deleteRelay(w, r, id)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// writeError writes a JSON error response
func writeError(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// writeJSON writes a JSON success response
func writeJSON(w http.ResponseWriter, status int, data map[string]string) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// scheme returns the URL scheme based on the request
func scheme(r *http.Request) string {
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		return "https"
	}
	return "http"
}

// createRelay creates a new relay config file
func (api *APIHandler) createRelay(w http.ResponseWriter, r *http.Request, id string) {
	configPath := api.configPath(id)

	if _, err := os.Stat(configPath); err == nil {
		writeError(w, http.StatusConflict, "relay with this id already exists")
		return
	}

	config, err := api.parseAndValidateConfig(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := api.checkDuplicateSchemaOrHost(config, ""); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	if err := api.saveConfig(configPath, config); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to write config: %v", err))
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"message": "relay created successfully"})
}

// updateRelay updates an existing relay config file
func (api *APIHandler) updateRelay(w http.ResponseWriter, r *http.Request, id string) {
	configPath := api.configPath(id)

	if err := api.checkConfigExists(configPath); err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "relay not found")
		} else {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to check config: %v", err))
		}
		return
	}

	config, err := api.parseAndValidateConfig(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := api.checkDuplicateSchemaOrHost(config, id+".toml"); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	if err := api.saveConfig(configPath, config); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to write config: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "relay updated successfully"})
}

// patchRelay partially updates an existing relay config
func (api *APIHandler) patchRelay(w http.ResponseWriter, r *http.Request, id string) {
	configPath := api.configPath(id)

	if err := api.checkConfigExists(configPath); err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "relay not found")
		} else {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to check config: %v", err))
		}
		return
	}

	// Load existing config
	existing, err := api.loadConfigFromPath(configPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to read existing config: %v", err))
		return
	}

	// Parse patch
	patch, err := api.readPatch(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Apply patch to existing config
	if err := api.applyPatch(existing, patch); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Validate the patched config
	if err := api.validatePatchedConfig(existing); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := api.checkDuplicateSchemaOrHost(existing, id+".toml"); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	if err := api.saveConfig(configPath, existing); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to write config: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "relay patched successfully"})
}

// readPatch reads and parses the patch JSON from the request
func (api *APIHandler) readPatch(r *http.Request) (map[string]interface{}, error) {
	r.Body = http.MaxBytesReader(nil, r.Body, 1024*1024)
	defer r.Body.Close()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read body: %w", err)
	}

	var patch map[string]interface{}
	if err := json.Unmarshal(body, &patch); err != nil {
		return nil, fmt.Errorf("invalid json: %w", err)
	}

	return patch, nil
}

// applyPatch applies a JSON patch to a config using reflection via JSON marshaling
func (api *APIHandler) applyPatch(config *Config, patch map[string]interface{}) error {
	// Convert config to map for merging
	configJSON, _ := json.Marshal(config)
	var configMap map[string]interface{}
	json.Unmarshal(configJSON, &configMap)

	// Merge patch
	merged := deepMerge(configMap, patch)

	// Convert back to a new config (don't modify original until validation passes)
	mergedJSON, _ := json.Marshal(merged)
	var patched Config
	if err := json.Unmarshal(mergedJSON, &patched); err != nil {
		return err
	}

	// Copy patched values to original config
	*config = patched
	return nil
}

// deepMerge recursively merges patch into base
func deepMerge(base, patch map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	for k, v := range base {
		result[k] = v
	}

	for k, v := range patch {
		if v == nil {
			delete(result, k)
		} else if patchMap, ok := v.(map[string]interface{}); ok {
			if baseMap, ok := base[k].(map[string]interface{}); ok {
				result[k] = deepMerge(baseMap, patchMap)
			} else {
				result[k] = v
			}
		} else {
			result[k] = v
		}
	}

	return result
}

// validatePatchedConfig validates a config after patching
func (api *APIHandler) validatePatchedConfig(config *Config) error {
	if config.Host == "" {
		return fmt.Errorf("host is required")
	}
	if config.Schema == "" {
		return fmt.Errorf("schema is required")
	}
	if !regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`).MatchString(config.Schema) {
		return fmt.Errorf("schema must contain only letters, numbers, and underscores")
	}
	if config.Secret == "" {
		return fmt.Errorf("secret is required")
	}
	if _, err := nostr.SecretKeyFromHex(config.Secret); err != nil {
		return fmt.Errorf("invalid secret key: %w", err)
	}
	if config.Info.Pubkey == "" {
		return fmt.Errorf("info.pubkey is required")
	}
	if _, err := nostr.PubKeyFromHex(config.Info.Pubkey); err != nil {
		return fmt.Errorf("invalid info.pubkey: %w", err)
	}
	return nil
}

// deleteRelay deletes a relay config file
func (api *APIHandler) deleteRelay(w http.ResponseWriter, r *http.Request, id string) {
	configPath := api.configPath(id)

	if err := api.checkConfigExists(configPath); err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "relay not found")
		} else {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to check config: %v", err))
		}
		return
	}

	if err := os.Remove(configPath); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to delete config: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "relay deleted successfully"})
}

// configPath returns the full path for a config file
func (api *APIHandler) configPath(id string) string {
	return filepath.Join(api.configDir, id+".toml")
}

// checkConfigExists checks if a config file exists
func (api *APIHandler) checkConfigExists(path string) error {
	_, err := os.Stat(path)
	return err
}

// loadConfigFromPath loads a config from a file path
func (api *APIHandler) loadConfigFromPath(path string) (*Config, error) {
	var config Config
	_, err := toml.DecodeFile(path, &config)
	if err != nil {
		return nil, err
	}
	return &config, nil
}

// parseAndValidateConfig parses and validates the JSON config from the request body
func (api *APIHandler) parseAndValidateConfig(r *http.Request) (*Config, error) {
	r.Body = http.MaxBytesReader(nil, r.Body, 1024*1024)
	defer r.Body.Close()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read body: %w", err)
	}

	var config Config
	if err := json.Unmarshal(body, &config); err != nil {
		return nil, fmt.Errorf("invalid json config: %w", err)
	}

	if err := api.validatePatchedConfig(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

// saveConfig saves a config to a file as TOML
func (api *APIHandler) saveConfig(path string, config *Config) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	encoder := toml.NewEncoder(file)
	if err := encoder.Encode(config); err != nil {
		return fmt.Errorf("failed to encode toml: %w", err)
	}

	return nil
}

// checkDuplicateSchemaOrHost checks if the schema or host is already in use by another config
func (api *APIHandler) checkDuplicateSchemaOrHost(config *Config, excludeFilename string) error {
	entries, err := os.ReadDir(api.configDir)
	if err != nil {
		return fmt.Errorf("failed to read config directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == excludeFilename || !strings.HasSuffix(entry.Name(), ".toml") {
			continue
		}

		path := filepath.Join(api.configDir, entry.Name())
		var existing Config
		if _, err := toml.DecodeFile(path, &existing); err != nil {
			continue
		}

		if existing.Schema == config.Schema {
			return fmt.Errorf("schema %q is already in use", config.Schema)
		}
		if existing.Host == config.Host {
			return fmt.Errorf("host %q is already in use", config.Host)
		}
	}

	return nil
}
