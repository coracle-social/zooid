package zooid

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"fiatjaf.com/nostr"
	"github.com/BurntSushi/toml"
)

// APIHandler handles REST API requests for managing virtual relays
type APIHandler struct {
	whitelist map[string]bool
	configDir string
}

// RelayConfigJSON is the JSON schema for relay configuration input
type RelayConfigJSON struct {
	Host   string `json:"host"`
	Schema string `json:"schema"`
	Secret string `json:"secret"`
	Info   struct {
		Name        string `json:"name"`
		Icon        string `json:"icon"`
		Pubkey      string `json:"pubkey"`
		Description string `json:"description"`
	} `json:"info"`
	Policy struct {
		PublicJoin      bool `json:"public_join"`
		StripSignatures bool `json:"strip_signatures"`
	} `json:"policy"`
	Groups struct {
		Enabled  bool `json:"enabled"`
		AutoJoin bool `json:"auto_join"`
	} `json:"groups"`
	Push struct {
		Enabled bool `json:"enabled"`
	} `json:"push"`
	Management struct {
		Enabled bool     `json:"enabled"`
		Methods []string `json:"methods"`
	} `json:"management"`
	Blossom struct {
		Enabled bool `json:"enabled"`
	} `json:"blossom"`
	Roles map[string]Role `json:"roles"`
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
	// Set JSON content type
	w.Header().Set("Content-Type", "application/json")

	// Authenticate the request using NIP-98
	pubkey, err := api.authenticateNIP98(r)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Check if pubkey is in whitelist
	if !api.whitelist[pubkey.Hex()] {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "pubkey not in whitelist"})
		return
	}

	// Route the request
	path := strings.TrimPrefix(r.URL.Path, "/")
	parts := strings.Split(path, "/")

	if len(parts) < 2 || parts[0] != "relay" {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
		return
	}

	id := parts[1]
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "relay id is required"})
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
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
	}
}

// authenticateNIP98 validates NIP-98 HTTP AUTH
func (api *APIHandler) authenticateNIP98(r *http.Request) (nostr.PubKey, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return nostr.PubKey{}, fmt.Errorf("missing authorization header")
	}

	// Parse the Authorization header
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "nostr" {
		return nostr.PubKey{}, fmt.Errorf("invalid authorization header format")
	}

	// Decode the base64 event
	eventJSON, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return nostr.PubKey{}, fmt.Errorf("invalid base64 encoding: %w", err)
	}

	// Parse the event
	var event nostr.Event
	if err := json.Unmarshal(eventJSON, &event); err != nil {
		return nostr.PubKey{}, fmt.Errorf("invalid event json: %w", err)
	}

	// Verify the event kind is HTTP Auth (27235)
	if event.Kind != nostr.KindHTTPAuth {
		return nostr.PubKey{}, fmt.Errorf("invalid event kind: expected %d, got %d", nostr.KindHTTPAuth, event.Kind)
	}

	// Verify the event signature
	if !event.VerifySignature() {
		return nostr.PubKey{}, fmt.Errorf("invalid event signature")
	}

	// Verify the event tags contain the correct URL and method
	var hasURL, hasMethod bool
	expectedURL := fmt.Sprintf("%s://%s%s", scheme(r), r.Host, r.URL.Path)

	for _, tag := range event.Tags {
		if len(tag) < 2 {
			continue
		}
		switch tag[0] {
		case "u":
			if tag[1] == expectedURL {
				hasURL = true
			}
		case "method":
			if strings.ToUpper(tag[1]) == r.Method {
				hasMethod = true
			}
		}
	}

	if !hasURL {
		return nostr.PubKey{}, fmt.Errorf("event missing or invalid u tag")
	}
	if !hasMethod {
		return nostr.PubKey{}, fmt.Errorf("event missing or invalid method tag")
	}

	return event.PubKey, nil
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
	configPath := filepath.Join(api.configDir, id+".toml")

	// Check if file already exists
	if _, err := os.Stat(configPath); err == nil {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": "relay with this id already exists"})
		return
	}

	// Parse and validate the JSON config
	config, err := api.parseAndValidateConfig(r)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Check for duplicate schema or host
	if err := api.checkDuplicateSchemaOrHost(config, ""); err != nil {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Convert to TOML and write
	if err := api.writeConfigAsTOML(configPath, config); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("failed to write config: %v", err)})
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"message": "relay created successfully"})
}

// updateRelay updates an existing relay config file
func (api *APIHandler) updateRelay(w http.ResponseWriter, r *http.Request, id string) {
	configPath := filepath.Join(api.configDir, id+".toml")

	// Check if file exists
	if _, err := os.Stat(configPath); err != nil {
		if os.IsNotExist(err) {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "relay not found"})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("failed to check config: %v", err)})
		return
	}

	// Parse and validate the JSON config
	config, err := api.parseAndValidateConfig(r)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Check for duplicate schema or host (excluding this config file)
	if err := api.checkDuplicateSchemaOrHost(config, id+".toml"); err != nil {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Convert to TOML and write
	if err := api.writeConfigAsTOML(configPath, config); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("failed to write config: %v", err)})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "relay updated successfully"})
}

// patchRelay partially updates an existing relay config by recursively merging changes
func (api *APIHandler) patchRelay(w http.ResponseWriter, r *http.Request, id string) {
	configPath := filepath.Join(api.configDir, id+".toml")

	// Check if file exists
	if _, err := os.Stat(configPath); err != nil {
		if os.IsNotExist(err) {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "relay not found"})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("failed to check config: %v", err)})
		return
	}

	// Read existing config
	var existingConfig RelayConfigJSON
	if _, err := toml.DecodeFile(configPath, &existingConfig); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("failed to read existing config: %v", err)})
		return
	}

	// Read and parse the patch
	r.Body = http.MaxBytesReader(nil, r.Body, 1024*1024) // 1MB limit
	defer r.Body.Close()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("failed to read body: %v", err)})
		return
	}

	var patch map[string]interface{}
	if err := json.Unmarshal(body, &patch); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("invalid json: %v", err)})
		return
	}

	// Convert existing config to map for merging
	existingJSON, _ := json.Marshal(existingConfig)
	var existingMap map[string]interface{}
	json.Unmarshal(existingJSON, &existingMap)

	// Recursively merge patch into existing
	mergedMap := deepMerge(existingMap, patch)

	// Convert back to RelayConfigJSON
	mergedJSON, _ := json.Marshal(mergedMap)
	var mergedConfig RelayConfigJSON
	if err := json.Unmarshal(mergedJSON, &mergedConfig); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("failed to process merged config: %v", err)})
		return
	}

	// Validate required fields are still present
	if mergedConfig.Host == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "host is required"})
		return
	}
	if mergedConfig.Schema == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "schema is required"})
		return
	}
	if mergedConfig.Secret == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "secret is required"})
		return
	}

	// Validate the secret key
	if _, err := nostr.SecretKeyFromHex(mergedConfig.Secret); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("invalid secret key: %v", err)})
		return
	}

	// Validate info.pubkey if provided
	if mergedConfig.Info.Pubkey != "" {
		if _, err := nostr.PubKeyFromHex(mergedConfig.Info.Pubkey); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("invalid info.pubkey: %v", err)})
			return
		}
	}

	// Check for duplicate schema or host (excluding this config file)
	if err := api.checkDuplicateSchemaOrHost(&mergedConfig, id+".toml"); err != nil {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Convert to TOML and write
	if err := api.writeConfigAsTOML(configPath, &mergedConfig); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("failed to write config: %v", err)})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "relay patched successfully"})
}

// deepMerge recursively merges patch into base
func deepMerge(base, patch map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	// Copy all values from base
	for k, v := range base {
		result[k] = v
	}

	// Apply patches
	for k, v := range patch {
		if v == nil {
			// Remove key if explicitly set to null
			delete(result, k)
		} else if patchMap, ok := v.(map[string]interface{}); ok {
			// Recursively merge nested maps
			if baseMap, ok := base[k].(map[string]interface{}); ok {
				result[k] = deepMerge(baseMap, patchMap)
			} else {
				result[k] = v
			}
		} else {
			// Replace value
			result[k] = v
		}
	}

	return result
}

// deleteRelay deletes a relay config file
func (api *APIHandler) deleteRelay(w http.ResponseWriter, r *http.Request, id string) {
	configPath := filepath.Join(api.configDir, id+".toml")

	// Check if file exists
	if _, err := os.Stat(configPath); err != nil {
		if os.IsNotExist(err) {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "relay not found"})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("failed to check config: %v", err)})
		return
	}

	// Delete the config file
	if err := os.Remove(configPath); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("failed to delete config: %v", err)})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "relay deleted successfully"})
}

// parseAndValidateConfig parses and validates the JSON config from the request body
func (api *APIHandler) parseAndValidateConfig(r *http.Request) (*RelayConfigJSON, error) {
	// Limit body size to prevent abuse
	r.Body = http.MaxBytesReader(nil, r.Body, 1024*1024) // 1MB limit
	defer r.Body.Close()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read body: %w", err)
	}

	// Parse the JSON config
	var config RelayConfigJSON
	if err := json.Unmarshal(body, &config); err != nil {
		return nil, fmt.Errorf("invalid json config: %w", err)
	}

	// Validate required fields
	if config.Host == "" {
		return nil, fmt.Errorf("host is required")
	}
	if config.Schema == "" {
		return nil, fmt.Errorf("schema is required")
	}
	if config.Secret == "" {
		return nil, fmt.Errorf("secret is required")
	}

	// Validate the secret key
	if _, err := nostr.SecretKeyFromHex(config.Secret); err != nil {
		return nil, fmt.Errorf("invalid secret key: %w", err)
	}

	// Validate info.pubkey if provided
	if config.Info.Pubkey != "" {
		if _, err := nostr.PubKeyFromHex(config.Info.Pubkey); err != nil {
			return nil, fmt.Errorf("invalid info.pubkey: %w", err)
		}
	}

	return &config, nil
}

// writeConfigAsTOML writes the config as TOML to the given path
func (api *APIHandler) writeConfigAsTOML(path string, config *RelayConfigJSON) error {
	// Convert JSON config to TOML-compatible struct
	tomlConfig := struct {
		Host   string `toml:"host"`
		Schema string `toml:"schema"`
		Secret string `toml:"secret"`
		Info   struct {
			Name        string `toml:"name"`
			Icon        string `toml:"icon"`
			Pubkey      string `toml:"pubkey"`
			Description string `toml:"description"`
		} `toml:"info"`
		Policy struct {
			PublicJoin      bool `toml:"public_join"`
			StripSignatures bool `toml:"strip_signatures"`
		} `toml:"policy"`
		Groups struct {
			Enabled  bool `toml:"enabled"`
			AutoJoin bool `toml:"auto_join"`
		} `toml:"groups"`
		Push struct {
			Enabled bool `toml:"enabled"`
		} `toml:"push"`
		Management struct {
			Enabled bool     `toml:"enabled"`
			Methods []string `toml:"methods"`
		} `toml:"management"`
		Blossom struct {
			Enabled bool `toml:"enabled"`
		} `toml:"blossom"`
		Roles map[string]Role `toml:"roles"`
	}{
		Host:   config.Host,
		Schema: config.Schema,
		Secret: config.Secret,
		Info: struct {
			Name        string `toml:"name"`
			Icon        string `toml:"icon"`
			Pubkey      string `toml:"pubkey"`
			Description string `toml:"description"`
		}{
			Name:        config.Info.Name,
			Icon:        config.Info.Icon,
			Pubkey:      config.Info.Pubkey,
			Description: config.Info.Description,
		},
		Policy: struct {
			PublicJoin      bool `toml:"public_join"`
			StripSignatures bool `toml:"strip_signatures"`
		}{
			PublicJoin:      config.Policy.PublicJoin,
			StripSignatures: config.Policy.StripSignatures,
		},
		Groups: struct {
			Enabled  bool `toml:"enabled"`
			AutoJoin bool `toml:"auto_join"`
		}{
			Enabled:  config.Groups.Enabled,
			AutoJoin: config.Groups.AutoJoin,
		},
		Push: struct {
			Enabled bool `toml:"enabled"`
		}{
			Enabled: config.Push.Enabled,
		},
		Management: struct {
			Enabled bool     `toml:"enabled"`
			Methods []string `toml:"methods"`
		}{
			Enabled: config.Management.Enabled,
			Methods: config.Management.Methods,
		},
		Blossom: struct {
			Enabled bool `toml:"enabled"`
		}{
			Enabled: config.Blossom.Enabled,
		},
		Roles: config.Roles,
	}

	// Encode to TOML
	var buf bytes.Buffer
	encoder := toml.NewEncoder(&buf)
	if err := encoder.Encode(tomlConfig); err != nil {
		return fmt.Errorf("failed to encode toml: %w", err)
	}

	// Write to file
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// checkDuplicateSchemaOrHost checks if the schema or host is already in use by another config
func (api *APIHandler) checkDuplicateSchemaOrHost(config *RelayConfigJSON, excludeFilename string) error {
	entries, err := os.ReadDir(api.configDir)
	if err != nil {
		return fmt.Errorf("failed to read config directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if entry.Name() == excludeFilename {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".toml") {
			continue
		}

		path := filepath.Join(api.configDir, entry.Name())
		var existingConfig Config
		if _, err := toml.DecodeFile(path, &existingConfig); err != nil {
			continue // Skip invalid configs
		}

		if existingConfig.Schema == config.Schema {
			return fmt.Errorf("schema %q is already in use", config.Schema)
		}
		if existingConfig.Host == config.Host {
			return fmt.Errorf("host %q is already in use", config.Host)
		}
	}

	return nil
}
