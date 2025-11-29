//go:build darwin

package host

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func ensureSecretsFile(outputDir string) (string, error) {
	secretsDir := filepath.Join(outputDir, "secrets")
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		return "", err
	}
	secretsFile := filepath.Join(secretsDir, "users.csv")
	if _, err := os.Stat(secretsFile); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if err := os.WriteFile(secretsFile, []byte("username,password\n"), 0o600); err != nil {
			return "", err
		}
	} else {
		fi, err := os.Stat(secretsFile)
		if err == nil && fi.Size() == 0 {
			if err := os.WriteFile(secretsFile, []byte("username,password\n"), 0o600); err != nil {
				return "", err
			}
		}
	}
	if err := os.Chmod(secretsFile, 0o600); err != nil {
		return "", err
	}
	return secretsFile, nil
}

func appendPassword(secretsFile, username, password string) error {
	f, err := os.OpenFile(secretsFile, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if _, err := fmt.Fprintf(f, "%s,%s\n", username, password); err != nil {
		return err
	}
	return nil
}

func generatePassword(defaultPassword string) (string, error) {
	if defaultPassword != "" {
		return defaultPassword, nil
	}

	charSets := []string{
		"ABCDEFGHJKMNPQRSTUVWXYZ", // upper (no O/I)
		"abcdefghjkmnpqrstuvwxyz", // lower (no o/l)
		"23456789",                // digits (no 0/1)
		"!@#$%^&*",                // special
	}

	var pwd []byte
	for i := 0; i < 4; i++ {
		for _, set := range charSets {
			idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(set))))
			if err != nil {
				return "", err
			}
			pwd = append(pwd, set[idx.Int64()])
		}
	}
	return string(pwd), nil
}

func systemUserExists(ctx context.Context, username string) (bool, error) {
	cmd := exec.CommandContext(ctx, "id", "-u", username)
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func createSystemUser(ctx context.Context, username, password string) error {
	homeDir := filepath.Join("/Users", username)
	cmd := exec.CommandContext(ctx, "sysadminctl",
		"-addUser", username,
		"-fullName", username,
		"-password", password,
		"-home", homeDir,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("create user %s: %w (output=%s)", username, err, strings.TrimSpace(string(output)))
	}
	return nil
}
