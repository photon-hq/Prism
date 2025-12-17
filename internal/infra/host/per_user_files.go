//go:build darwin

package host

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"prism/internal/infra/config"
	"prism/internal/infra/state"
)

const (
	envFRPCToken   = "FRPC_TOKEN"
	envGITHUBToken = "GITHUB_TOKEN"
)

// generateSubdomain returns a random lower-case alpha-numeric string of the
// given length, suitable for use as a subdomain prefix. Ambiguous characters
// (0/1 and i/l/o) are excluded to improve readability.
func generateSubdomain(n int) (string, error) {
	if n <= 0 {
		return "", errors.New("subdomain length must be positive")
	}
	const letters = "abcdefghjkmnpqrstuvwxyz23456789"
	max := big.NewInt(int64(len(letters)))
	b := make([]byte, n)
	for i := 0; i < n; i++ {
		r, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b[i] = letters[r.Int64()]
	}
	return string(b), nil
}

// ensureServiceArchive downloads (or reuses cached) service bundle and
// extracts it into output/cache/imsg.
func ensureServiceArchive(ctx context.Context, cfg config.Config, outputDir string) (string, error) {
	cacheDir := filepath.Join(outputDir, "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}
	archivePath := filepath.Join(cacheDir, "bundle-macos-arm64.tar.gz")
	if _, err := os.Stat(archivePath); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		resolvedURL, err := resolveArchiveURL(ctx, cfg.Globals.Service.ArchiveURL)
		if err != nil {
			return "", err
		}
		if err := downloadArchive(ctx, resolvedURL, archivePath); err != nil {
			return "", err
		}
	}

	extractDir := filepath.Join(cacheDir, "imsg")
	_ = os.RemoveAll(extractDir)
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, "tar", "-xzf", archivePath, "-C", extractDir, "--strip-components=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("extract archive: %w (output=%s)", err, strings.TrimSpace(string(out)))
	}
	return extractDir, nil
}

func refreshServiceArchive(ctx context.Context, cfg config.Config, outputDir string) (string, error) {
	if strings.TrimSpace(outputDir) == "" {
		return "", errors.New("outputDir is empty")
	}
	cacheDir := filepath.Join(outputDir, "cache")
	archivePath := filepath.Join(cacheDir, "bundle-macos-arm64.tar.gz")
	_ = os.Remove(archivePath)
	return ensureServiceArchive(ctx, cfg, outputDir)
}

func downloadArchive(ctx context.Context, urlStr, dest string) error {
	if strings.TrimSpace(urlStr) == "" {
		return errors.New("globals.service.archive_url is empty")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return err
	}
	token := strings.TrimSpace(os.Getenv(envGITHUBToken))
	if token != "" {
		if parsed, err := url.Parse(urlStr); err == nil {
			host := strings.ToLower(parsed.Host)
			if strings.Contains(host, "github.com") || strings.Contains(host, "raw.githubusercontent.com") {
				req.Header.Set("Authorization", "Bearer "+token)
				// For GitHub API asset downloads, we need the Accept header
				if strings.Contains(urlStr, "api.github.com") && strings.Contains(urlStr, "/releases/assets/") {
					req.Header.Set("Accept", "application/octet-stream")
				}
			}
		}
	}
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download archive: unexpected status %s", resp.Status)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return err
	}
	return nil
}

// chownRecursive sets the ownership of the given path (recursively) to the
// specified username when running as root. In non-root environments (for
// example, tests) it becomes a no-op.
func chownRecursive(username, path string) error {
	if strings.TrimSpace(username) == "" || strings.TrimSpace(path) == "" {
		return nil
	}
	// Only attempt chown when running as root.
	if os.Geteuid() != 0 {
		return nil
	}

	cmd := exec.Command("chown", "-R", username, path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		lower := strings.ToLower(string(out))
		if strings.Contains(lower, "operation not permitted") || strings.Contains(lower, "permission denied") {
			// Best-effort: do not fail provisioning in environments where chown is
			// restricted.
			return nil
		}
		return fmt.Errorf("chown -R %s %s: %w (output=%s)", username, path, err, strings.TrimSpace(string(out)))
	}

	return nil
}

// resolveArchiveURL resolves archive URL (supports gh://owner/repo/asset shorthand).
func resolveArchiveURL(ctx context.Context, urlStr string) (string, error) {
	s := strings.TrimSpace(urlStr)
	if s == "" {
		return "", errors.New("globals.service.archive_url is empty")
	}

	const ghPrefix = "gh://"
	if !strings.HasPrefix(s, ghPrefix) {
		// Normal URL; use as-is.
		return s, nil
	}

	spec := strings.TrimPrefix(s, ghPrefix)
	parts := strings.SplitN(spec, "/", 3)
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid GitHub archive_url %q (expected gh://owner/repo/asset-name)", urlStr)
	}
	owner, repo, assetSpec := parts[0], parts[1], parts[2]
	assetName := assetSpec
	tag := ""
	if idx := strings.Index(assetSpec, "@"); idx >= 0 {
		assetName = assetSpec[:idx]
		tag = strings.TrimSpace(assetSpec[idx+1:])
	}
	var apiURL string
	if tag == "" {
		apiURL = fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)
	} else {
		apiURL = fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/tags/%s", owner, repo, tag)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", err
	}

	token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("resolve GitHub release: unexpected status %s", resp.Status)
	}

	var rel struct {
		Assets []struct {
			ID                 int    `json:"id"`
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
			URL                string `json:"url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", err
	}

	for _, a := range rel.Assets {
		if a.Name == assetName {
			// For private repositories, browser_download_url doesn't work with token auth.
			// Use the API URL instead, which supports proper authentication.
			u := strings.TrimSpace(a.URL)
			if u == "" {
				break
			}
			return u, nil
		}
	}

	if tag == "" {
		return "", fmt.Errorf("resolve GitHub release: asset %q not found in latest release", assetName)
	}
	return "", fmt.Errorf("resolve GitHub release: asset %q not found in release %q", assetName, tag)
}

// ensurePerUserFiles prepares the per-user services/imsg directory, including
// config.json, frpc.toml and the per-user prism wrapper.
func ensurePerUserFiles(
	cfg config.Config,
	username string,
	localPort int,
	extractDir string,
	prismPath string,
) (state.User, error) {
	homeDir := filepath.Join("/Users", username)
	serviceDir := filepath.Join(homeDir, "services", "imsg")
	if err := copyDir(extractDir, serviceDir); err != nil {
		return state.User{}, err
	}

	configPath := filepath.Join(serviceDir, "config.json")
	var ucfg struct {
		Username   string `json:"username"`
		MachineID  string `json:"machine_id"`
		LocalPort  int    `json:"local_port"`
		Subdomain  string `json:"subdomain"`
		FullDomain string `json:"full_domain"`
		FRPCConfig string `json:"frpc_config"`
		NexusAddr  string `json:"nexus_addr"`
	}
	if data, err := os.ReadFile(configPath); err == nil {
		_ = json.Unmarshal(data, &ucfg)
	}

	subdomain := strings.TrimSpace(ucfg.Subdomain)
	if subdomain == "" {
		var err error
		subdomain, err = generateSubdomain(6)
		if err != nil {
			return state.User{}, err
		}
	}
	fullDomain := fmt.Sprintf("%s.%s", subdomain, cfg.Globals.DomainSuffix)

	ucfg.Username = username
	ucfg.MachineID = cfg.Globals.MachineID
	ucfg.LocalPort = localPort
	ucfg.Subdomain = subdomain
	ucfg.FullDomain = fullDomain
	ucfg.FRPCConfig = filepath.Join(serviceDir, "frpc.toml")
	if strings.TrimSpace(ucfg.NexusAddr) == "" {
		ucfg.NexusAddr = strings.TrimRight(cfg.Globals.Nexus.BaseURL, "/")
	}

	data, err := json.MarshalIndent(&ucfg, "", "  ")
	if err != nil {
		return state.User{}, err
	}
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		return state.User{}, err
	}

	frpcToml := fmt.Sprintf("serverAddr = \"%s\"\nserverPort = %d\n",
		cfg.Globals.FRPC.ServerAddr,
		cfg.Globals.FRPC.ServerPort,
	)

	if token := strings.TrimSpace(os.Getenv(envFRPCToken)); token != "" {
		frpcToml += fmt.Sprintf("\nauth.token = \"%s\"\n", token)
	}

	frpcToml += fmt.Sprintf("\n[[proxies]]\nname = \"%s-imsg\"\ntype = \"http\"\nlocalIP = \"127.0.0.1\"\nlocalPort = %d\nsubdomain = \"%s\"\nmetadatas = { friendlyName = \"\" }\n",
		username,
		localPort,
		subdomain,
	)
	if err := os.WriteFile(ucfg.FRPCConfig, []byte(frpcToml), 0o600); err != nil {
		return state.User{}, err
	}

	if prismPath != "" {
		localBin := filepath.Join(serviceDir, "prism-host")
		if err := copyExecutable(prismPath, localBin); err != nil {
			return state.User{}, err
		}

		wrapper := fmt.Sprintf("#!/bin/zsh\nexec \"%s\" user \"$@\"\n", localBin)
		wrapperPath := filepath.Join(serviceDir, "prism")
		if err := os.WriteFile(wrapperPath, []byte(wrapper), 0o755); err != nil {
			return state.User{}, err
		}
	}

	if err := chownRecursive(username, serviceDir); err != nil {
		return state.User{}, err
	}

	// Create LaunchDaemons for headless service startup at boot
	// Find frpc binary
	frpcBin, err := exec.LookPath("frpc")
	if err != nil {
		// Try common paths
		for _, p := range []string{"/opt/homebrew/bin/frpc", "/usr/local/bin/frpc"} {
			if _, err := os.Stat(p); err == nil {
				frpcBin = p
				break
			}
		}
		if frpcBin == "" {
			return state.User{}, fmt.Errorf("frpc binary not found")
		}
	}

	// Find server binary
	serverBin := filepath.Join(serviceDir, "iMessageKitServer.app", "Contents", "MacOS", "iMessageKitServer")
	if _, err := os.Stat(serverBin); err != nil {
		return state.User{}, fmt.Errorf("server binary not found: %w", err)
	}

	daemonCfg := UserLaunchDaemonConfig{
		Username:   username,
		HomeDir:    homeDir,
		ServiceDir: serviceDir,
		ServerBin:  serverBin,
		FRPCBin:    frpcBin,
		FRPCConfig: ucfg.FRPCConfig,
		LocalPort:  localPort,
		MachineID:  cfg.Globals.MachineID,
		NexusAddr:  ucfg.NexusAddr,
	}
	if err := EnsureUserLaunchDaemons(daemonCfg); err != nil {
		return state.User{}, fmt.Errorf("create LaunchDaemons: %w", err)
	}

	// Bootstrap the daemons so they start running
	if err := BootstrapUserLaunchDaemons(username); err != nil {
		return state.User{}, fmt.Errorf("bootstrap LaunchDaemons: %w", err)
	}

	return state.User{
		Name:      username,
		Port:      localPort,
		Subdomain: subdomain,
	}, nil
}

func syncServiceDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	args := []string{
		"-a",
		"--exclude", "config.json",
		"--exclude", "frpc.toml",
		"--exclude", "prism-host",
		"--exclude", "prism",
		src + "/",
		dst + "/",
	}
	cmd := exec.Command("rsync", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("rsync %s -> %s: %w (output=%s)", src, dst, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	cmd := exec.Command("rsync", "-a", src+"/", dst+"/")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("rsync %s -> %s: %w (output=%s)", src, dst, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open prism binary: %w", err)
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("create per-user prism binary: %w", err)
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy prism binary: %w", err)
	}

	return nil
}
