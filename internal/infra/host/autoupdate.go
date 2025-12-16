//go:build darwin

package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"prism/internal/infra/config"
	"prism/internal/infra/state"
)

const (
	versionFileName         = "current_version.txt"
	envGitHubTokenForUpdate = "GITHUB_TOKEN"

	// Retry configuration for GitHub API calls
	maxRetries     = 3
	initialBackoff = 1 * time.Second
	maxBackoff     = 30 * time.Second
)

// AutoUpdateConfig holds configuration for auto-update behavior.
type AutoUpdateConfig struct {
	CheckInterval time.Duration
	OutputDir     string
	ConfigPath    string
	StatePath     string
}

// githubRelease represents the relevant fields from GitHub API response.
type githubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
	} `json:"assets"`
}

// RunAutoUpdateLoop starts the auto-update daemon loop.
// It checks for new server releases at the configured interval and updates all users if needed.
func RunAutoUpdateLoop(ctx context.Context, auCfg AutoUpdateConfig) {
	// Ensure minimum interval to prevent CPU spinning
	interval := auCfg.CheckInterval
	if interval < time.Minute {
		interval = time.Hour
		log.Printf("[autoupdate] check interval too short, using default 1 hour")
	}

	log.Printf("[autoupdate] starting auto-update loop (interval=%s)", interval)

	// Run once immediately at startup
	if err := checkAndUpdate(ctx, auCfg); err != nil {
		log.Printf("[autoupdate] initial check failed: %v", err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("[autoupdate] stopping auto-update loop")
			return
		case <-ticker.C:
			if err := checkAndUpdate(ctx, auCfg); err != nil {
				log.Printf("[autoupdate] check failed: %v", err)
			}
		}
	}
}

// checkAndUpdate checks for a new server version and updates if available.
func checkAndUpdate(ctx context.Context, auCfg AutoUpdateConfig) error {
	cfg, err := config.Load(auCfg.ConfigPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	st, err := state.Load(auCfg.StatePath)
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}

	if len(st.Users) == 0 {
		log.Printf("[autoupdate] no users in state; skipping update check")
		return nil
	}

	archiveURL := strings.TrimSpace(cfg.Globals.Service.ArchiveURL)
	if archiveURL == "" {
		return errors.New("globals.service.archive_url is empty")
	}

	// Only support gh:// URLs for auto-update (need tag comparison)
	if !strings.HasPrefix(archiveURL, "gh://") {
		log.Printf("[autoupdate] archive_url is not a gh:// URL; skipping auto-update")
		return nil
	}

	latestTag, err := fetchLatestRelease(ctx, archiveURL)
	if err != nil {
		return fmt.Errorf("fetch latest release: %w", err)
	}

	// Empty tag means fixed version specified, skip auto-update
	if latestTag == "" {
		return nil
	}

	currentTag, err := readCurrentVersion(auCfg.OutputDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read current version: %w", err)
	}

	if currentTag == latestTag {
		log.Printf("[autoupdate] already on latest version %s", latestTag)
		return nil
	}

	log.Printf("[autoupdate] new version available: %s -> %s", currentTag, latestTag)

	// Perform the update
	updatedCount, err := performUpdate(ctx, cfg, st, auCfg.OutputDir)
	if err != nil {
		return fmt.Errorf("perform update: %w", err)
	}

	// Only save version if at least one user was updated successfully
	if updatedCount == 0 {
		return fmt.Errorf("no users were updated successfully")
	}

	// Save the new version
	if err := writeCurrentVersion(auCfg.OutputDir, latestTag); err != nil {
		return fmt.Errorf("write current version: %w", err)
	}

	log.Printf("[autoupdate] successfully updated to version %s", latestTag)
	return nil
}

// fetchLatestRelease gets the latest release tag from GitHub with retry.
// Returns the tag name and an error. If a fixed tag is specified in the URL,
// returns empty string to signal that auto-update should be skipped.
func fetchLatestRelease(ctx context.Context, ghURL string) (string, error) {
	spec := strings.TrimPrefix(ghURL, "gh://")
	parts := strings.SplitN(spec, "/", 3)
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid gh:// URL: %s", ghURL)
	}

	owner, repo, assetSpec := parts[0], parts[1], parts[2]
	assetName := assetSpec
	fixedTag := ""

	if idx := strings.Index(assetSpec, "@"); idx >= 0 {
		assetName = assetSpec[:idx]
		fixedTag = strings.TrimSpace(assetSpec[idx+1:])
	}

	// If a fixed tag is specified, no auto-update needed
	if fixedTag != "" {
		log.Printf("[autoupdate] fixed tag %s specified; skipping auto-update", fixedTag)
		return "", nil
	}

	var lastErr error
	backoff := initialBackoff

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("[autoupdate] retry %d/%d after %v", attempt, maxRetries, backoff)
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(backoff):
			}
			// Exponential backoff with cap
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}

		tag, retryable, err := doFetchLatestRelease(ctx, owner, repo, assetName)
		if err == nil {
			return tag, nil
		}
		lastErr = err
		if !retryable {
			return "", err
		}
	}

	return "", fmt.Errorf("after %d retries: %w", maxRetries, lastErr)
}

// doFetchLatestRelease performs a single attempt to fetch the latest release.
// Returns (tag, retryable, error). If retryable is true, the caller may retry.
func doFetchLatestRelease(ctx context.Context, owner, repo, assetName string) (string, bool, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", false, err
	}

	if token := strings.TrimSpace(os.Getenv(envGitHubTokenForUpdate)); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		// Network errors are retryable
		return "", true, err
	}
	defer func() { _ = resp.Body.Close() }()

	// Handle rate limiting (429) and server errors (5xx) as retryable
	if resp.StatusCode == http.StatusTooManyRequests {
		return "", true, fmt.Errorf("GitHub API rate limited (429)")
	}
	if resp.StatusCode >= 500 {
		return "", true, fmt.Errorf("GitHub API server error: %s", resp.Status)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", false, fmt.Errorf("GitHub API returned status %s", resp.Status)
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", false, fmt.Errorf("decode release: %w", err)
	}

	// Verify the asset exists in this release
	assetFound := false
	for _, a := range rel.Assets {
		if a.Name == assetName {
			assetFound = true
			break
		}
	}

	if !assetFound {
		return "", false, fmt.Errorf("asset %q not found in release %s", assetName, rel.TagName)
	}

	return rel.TagName, false, nil
}

// performUpdate downloads the new version and updates all users.
// Returns the number of users successfully updated.
func performUpdate(ctx context.Context, cfg config.Config, st state.State, outputDir string) (int, error) {
	// Remove cached archive to force re-download
	cacheDir := filepath.Join(outputDir, "cache")
	archivePath := filepath.Join(cacheDir, "bundle-macos-arm64.tar.gz")
	_ = os.Remove(archivePath)

	// Download and extract new version
	extractDir, err := ensureServiceArchive(ctx, cfg, outputDir)
	if err != nil {
		return 0, fmt.Errorf("download/extract archive: %w", err)
	}

	updatedCount := 0

	// Update each user's service directory and restart
	for _, u := range st.Users {
		homeDir := filepath.Join("/Users", u.Name)
		serviceDir := filepath.Join(homeDir, "services", "imsg")

		// Check if service directory exists
		if _, err := os.Stat(serviceDir); err != nil {
			log.Printf("[autoupdate] user %s: service directory does not exist, skipping", u.Name)
			continue
		}

		// Sync the service files (excluding config files)
		if err := syncServiceDir(extractDir, serviceDir); err != nil {
			log.Printf("[autoupdate] user %s: sync failed: %v", u.Name, err)
			continue
		}

		// Fix ownership
		if err := chownRecursive(u.Name, serviceDir); err != nil {
			log.Printf("[autoupdate] user %s: chown failed: %v", u.Name, err)
			continue
		}

		// Restart the user's services
		if err := restartUserLaunchAgents(u.Name); err != nil {
			log.Printf("[autoupdate] user %s: restart failed: %v", u.Name, err)
			continue
		}

		log.Printf("[autoupdate] user %s: updated and restarted successfully", u.Name)
		updatedCount++
	}

	return updatedCount, nil
}

// readCurrentVersion reads the currently deployed version tag from file.
func readCurrentVersion(outputDir string) (string, error) {
	versionFile := filepath.Join(outputDir, "cache", versionFileName)
	data, err := os.ReadFile(versionFile)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// writeCurrentVersion saves the deployed version tag to file.
func writeCurrentVersion(outputDir string, tag string) error {
	cacheDir := filepath.Join(outputDir, "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return err
	}
	versionFile := filepath.Join(cacheDir, versionFileName)
	return os.WriteFile(versionFile, []byte(tag+"\n"), 0o644)
}
