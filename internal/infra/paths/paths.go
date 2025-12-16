package paths

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	envPrismConfig = "PRISM_CONFIG"
	envPrismState  = "PRISM_STATE"

	defaultConfigPath = "config/prism.json"
	defaultStatePath  = "output/state.json"
)

func ConfigPath() string {
	return resolvePath(envPrismConfig, defaultConfigPath)
}

func StatePath() string {
	return resolvePath(envPrismState, defaultStatePath)
}

func SecretsPath() string {
	state := StatePath()
	dir := filepath.Dir(state)
	return filepath.Join(dir, "secrets", "users.csv")
}

func OutputDir() string {
	state := StatePath()
	return filepath.Dir(state)
}

func resolvePath(envKey, defaultRel string) string {
	if v := strings.TrimSpace(os.Getenv(envKey)); v != "" {
		return makeAbsolute(v)
	}
	return makeAbsolute(defaultRel)
}

func makeAbsolute(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	cwd, err := os.Getwd()
	if err != nil {
		return p
	}
	return filepath.Join(cwd, p)
}
