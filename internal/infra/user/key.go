//go:build darwin

package userinfra

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"
)

// GetAPIKey requests a one-time API key from Nexus.
func GetAPIKey() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Sprintf("Failed to get API key: unable to determine user home directory: %v", err)
	}
	serviceDir := filepath.Join(home, "services", "imsg")
	configPath := filepath.Join(serviceDir, "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Sprintf("Failed to get API key: error reading config.json: %v", err)
	}

	var cfg struct {
		Username  string `json:"username"`
		MachineID string `json:"machine_id"`
		NexusAddr string `json:"nexus_addr"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Sprintf("Failed to get API key: error parsing config.json: %v", err)
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.NexusAddr), "/")
	if baseURL == "" {
		return "Failed to get API key: config.json is missing nexus_addr."
	}
	if strings.TrimSpace(cfg.MachineID) == "" {
		return "Failed to get API key: config.json is missing machine_id."
	}
	if strings.TrimSpace(cfg.Username) == "" {
		u, err := user.Current()
		if err != nil || strings.TrimSpace(u.Username) == "" {
			return "Failed to get API key: config.json is missing username and the system username could not be determined."
		}
		cfg.Username = u.Username
	}

	endpoint := baseURL + "/keys/create"
	payload := struct {
		MachineID string `json:"machineId"`
		UserID    string `json:"userId"`
	}{
		MachineID: cfg.MachineID,
		UserID:    cfg.Username,
	}
	body, err := json.Marshal(&payload)
	if err != nil {
		return fmt.Sprintf("Failed to get API key: error encoding request: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Sprintf("Failed to get API key: error constructing request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Sprintf("Failed to get API key: error calling Nexus: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Sprintf("Failed to get API key: Nexus returned status %s", resp.Status)
	}

	var decoded struct {
		OK     bool   `json:"ok"`
		Reason string `json:"reason"`
		APIKey string `json:"apiKey"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return fmt.Sprintf("Failed to get API key: error decoding response: %v", err)
	}
	if !decoded.OK {
		if strings.TrimSpace(decoded.Reason) == "" {
			decoded.Reason = "unknown-error"
		}
		return fmt.Sprintf("Failed to get API key: Nexus returned error: %s", decoded.Reason)
	}
	if strings.TrimSpace(decoded.APIKey) == "" {
		return "Failed to get API key: Nexus returned an empty apiKey."
	}

	return fmt.Sprintf(
		"One-time API key (displayed only once; please copy and store it securely now): %s",
		decoded.APIKey,
	)
}
