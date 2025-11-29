//go:build darwin

package host

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"prism/internal/infra/config"
	"prism/internal/infra/state"
)

// ProvisionUsers creates macOS users and prepares per-user service directories.
// Returns updated state and path to secrets file.
func ProvisionUsers(
	ctx context.Context,
	cfg config.Config,
	st state.State,
	userCount int,
	outputDir string,
	prismPath string,
) (state.State, string, error) {
	if userCount <= 0 {
		return st, "", errors.New("userCount must be positive")
	}

	if len(st.Users) > 0 {
		return st, "", errors.New("users already provisioned; please use the add-users flow instead")
	}

	machineID := strings.TrimSpace(cfg.Globals.MachineID)
	if machineID == "" {
		return st, "", errors.New("globals.machine_id is empty")
	}

	if outputDir == "" {
		return st, "", errors.New("outputDir is empty")
	}

	secretsFile, err := ensureSecretsFile(outputDir)
	if err != nil {
		return st, "", fmt.Errorf("ensure secrets file: %w", err)
	}

	extractDir, err := ensureServiceArchive(ctx, cfg, outputDir)
	if err != nil {
		return st, "", err
	}

	users := st.Users[:0]

	for i := 1; i <= userCount; i++ {
		username := fmt.Sprintf("%s-%d", machineID, i)
		localPort := cfg.Globals.Service.StartPort + i - 1

		exists, err := systemUserExists(ctx, username)
		if err != nil {
			return st, "", fmt.Errorf("check user %s: %w", username, err)
		}
		if exists {
			return st, "", fmt.Errorf("user %s already exists; please use the add-users flow instead of initial setup", username)
		}

		password, err := generatePassword(cfg.Globals.DefaultPassword)
		if err != nil {
			return st, "", fmt.Errorf("generate password for %s: %w", username, err)
		}

		if err := createSystemUser(ctx, username, password); err != nil {
			return st, "", err
		}

		if err := appendPassword(secretsFile, username, password); err != nil {
			return st, "", fmt.Errorf("save password for %s: %w", username, err)
		}

		u, err := ensurePerUserFiles(cfg, username, localPort, extractDir, prismPath)
		if err != nil {
			return st, "", err
		}

		users = append(users, u)
	}

	st.Users = users
	st.Initialized = true

	return st, secretsFile, nil
}

// AddUsers appends additional users on an already-initialized host.
func AddUsers(
	ctx context.Context,
	cfg config.Config,
	st state.State,
	userCount int,
	outputDir string,
	prismPath string,
) (state.State, string, error) {
	if userCount <= 0 {
		return st, "", errors.New("userCount must be positive")
	}

	if len(st.Users) == 0 {
		return st, "", errors.New("no existing users in state; please run initial setup before adding users")
	}

	machineID := strings.TrimSpace(cfg.Globals.MachineID)
	if machineID == "" {
		return st, "", errors.New("globals.machine_id is empty")
	}

	if outputDir == "" {
		return st, "", errors.New("outputDir is empty")
	}

	secretsFile, err := ensureSecretsFile(outputDir)
	if err != nil {
		return st, "", fmt.Errorf("ensure secrets file: %w", err)
	}

	extractDir, err := ensureServiceArchive(ctx, cfg, outputDir)
	if err != nil {
		return st, "", err
	}

	maxIndex := 0
	prefix := machineID + "-"
	for _, u := range st.Users {
		if !strings.HasPrefix(u.Name, prefix) {
			continue
		}
		suf := strings.TrimPrefix(u.Name, prefix)
		idx, err := strconv.Atoi(suf)
		if err != nil || idx <= 0 {
			continue
		}
		if idx > maxIndex {
			maxIndex = idx
		}
	}
	startIndex := maxIndex + 1

	users := st.Users

	for i := 0; i < userCount; i++ {
		idx := startIndex + i
		username := fmt.Sprintf("%s-%d", machineID, idx)
		localPort := cfg.Globals.Service.StartPort + idx - 1

		exists, err := systemUserExists(ctx, username)
		if err != nil {
			return st, "", fmt.Errorf("check user %s: %w", username, err)
		}
		if exists {
			return st, "", fmt.Errorf("user %s already exists; cannot add duplicate user", username)
		}

		password, err := generatePassword(cfg.Globals.DefaultPassword)
		if err != nil {
			return st, "", fmt.Errorf("generate password for %s: %w", username, err)
		}

		if err := createSystemUser(ctx, username, password); err != nil {
			return st, "", err
		}

		if err := appendPassword(secretsFile, username, password); err != nil {
			return st, "", fmt.Errorf("save password for %s: %w", username, err)
		}

		u, err := ensurePerUserFiles(cfg, username, localPort, extractDir, prismPath)
		if err != nil {
			return st, "", err
		}

		users = append(users, u)
	}

	st.Users = users
	st.Initialized = true

	return st, secretsFile, nil
}

// RemoveUser deletes a Prism-managed macOS user and removes it from state.
func RemoveUser(
	ctx context.Context,
	cfg config.Config,
	st state.State,
	username string,
	outputDir string,
) (state.State, error) {
	if strings.TrimSpace(username) == "" {
		return st, errors.New("username is empty")
	}

	machineID := strings.TrimSpace(cfg.Globals.MachineID)
	if machineID == "" {
		return st, errors.New("globals.machine_id is empty")
	}

	if outputDir == "" {
		return st, errors.New("outputDir is empty")
	}

	prefix := machineID + "-"
	if !strings.HasPrefix(username, prefix) {
		return st, fmt.Errorf("user %s does not belong to machine_id %s", username, machineID)
	}

	idx := -1
	for i, u := range st.Users {
		if u.Name == username {
			idx = i
			break
		}
	}
	if idx == -1 {
		return st, fmt.Errorf("user %s not found in state", username)
	}

	homeDir := filepath.Join("/Users", username)
	cmd := exec.CommandContext(ctx, "sysadminctl",
		"-deleteUser", username,
		"-home", homeDir,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return st, fmt.Errorf("delete user %s: %w (output=%s)", username, err, strings.TrimSpace(string(output)))
	}

	_ = os.RemoveAll(homeDir)

	users := make([]state.User, 0, len(st.Users)-1)
	for i, u := range st.Users {
		if i == idx {
			continue
		}
		users = append(users, u)
	}
	st.Users = users

	st.Initialized = true

	return st, nil
}
