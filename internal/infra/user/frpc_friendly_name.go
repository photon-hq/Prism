//go:build darwin

package userinfra

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	toml "github.com/pelletier/go-toml"
)

// chatDBAccountQuery extracts the user's phone number or email from Messages database.
// Priority: destination_caller_id (macOS 14+) > phone (P:) > email (E:/e:) > fallback.
// The destination_caller_id field contains the actual caller ID shown to recipients.
const chatDBAccountQuery = `
SELECT
  CASE
    WHEN destination_caller_id IS NOT NULL AND destination_caller_id != '' THEN destination_caller_id
    WHEN account LIKE 'P:%' THEN SUBSTR(account, 3)
    WHEN account LIKE 'E:%' THEN SUBSTR(account, 3)
    WHEN account LIKE 'e:%' THEN SUBSTR(account, 3)
    ELSE account
  END AS my_account
FROM message
WHERE is_from_me = 1
  AND (destination_caller_id IS NOT NULL OR account IS NOT NULL)
ORDER BY
  CASE 
    WHEN destination_caller_id IS NOT NULL AND destination_caller_id != '' THEN 0
    WHEN account LIKE 'P:%' THEN 1
    WHEN account LIKE 'E:%' THEN 2
    WHEN account LIKE 'e:%' THEN 2
    ELSE 3
  END,
  ROWID DESC
LIMIT 1;
`

func hasNonEmptyFriendlyName(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	tree, err := toml.LoadBytes(data)
	if err != nil {
		return false
	}

	raw := tree.Get("proxies")
	if raw == nil {
		return false
	}

	switch v := raw.(type) {
	case []*toml.Tree:
		for _, proxy := range v {
			if proxy == nil {
				continue
			}
			metaRaw := proxy.Get("metadatas")
			if metaRaw == nil {
				continue
			}
			metaTree, ok := metaRaw.(*toml.Tree)
			if !ok {
				continue
			}
			if val, ok := metaTree.Get("friendlyName").(string); ok && strings.TrimSpace(val) != "" {
				return true
			}
		}
	}

	return false
}

func autoDetectFriendlyName() string {
	aliasesOut, err := exec.Command("defaults", "read", "com.apple.madrid", "IMD-IDS-Aliases").CombinedOutput()
	if err == nil {
		if phone := extractPhone(string(aliasesOut)); phone != "" {
			return phone
		}
		if email := extractEmail(string(aliasesOut)); email != "" {
			return email
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	plistFiles := []string{
		filepath.Join(home, "Library", "Preferences", "com.apple.imservice.ids.iMessage.plist"),
		filepath.Join(home, "Library", "Preferences", "com.apple.imservice.ids.FaceTime.plist"),
		filepath.Join(home, "Library", "Preferences", "com.apple.madrid.plist"),
		filepath.Join(home, "Library", "Preferences", "com.apple.ids.plist"),
	}

	for _, pf := range plistFiles {
		if pf == "" {
			continue
		}
		if _, err := os.Stat(pf); err != nil {
			continue
		}
		out, err := exec.Command("plutil", "-p", pf).CombinedOutput()
		if err != nil {
			continue
		}
		if phone := extractPhone(string(out)); phone != "" {
			return phone
		}
		if email := extractEmail(string(out)); email != "" {
			return email
		}
	}

	if result := detectFromChatDB(home); result != "" {
		return result
	}

	return ""
}

func detectFromChatDB(home string) string {
	if home == "" {
		return ""
	}
	chatDB := filepath.Join(home, "Library", "Messages", "chat.db")
	if _, err := os.Stat(chatDB); err != nil {
		return ""
	}

	out, err := exec.Command("sqlite3", chatDB, chatDBAccountQuery).CombinedOutput()
	if err != nil {
		return ""
	}

	result := strings.TrimSpace(string(out))
	if result == "" {
		return ""
	}

	if phone := extractPhone(result); phone != "" {
		return phone
	}
	if email := extractEmail(result); email != "" {
		return email
	}

	if strings.Contains(result, "@") {
		return result
	}

	return ""
}

func extractPhone(s string) string {
	re := regexp.MustCompile(`\+[0-9]{7,15}`)
	return re.FindString(s)
}

func extractEmail(s string) string {
	re := regexp.MustCompile(`(?i)[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}`)
	return re.FindString(s)
}

func setFRPCFriendlyName(path, name string) error {
	if msg := validateFriendlyName(name); msg != "" {
		return fmt.Errorf("friendly name is invalid: %s", msg)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	tree, err := toml.LoadBytes(data)
	if err != nil {
		return fmt.Errorf("parse frpc.toml: %w", err)
	}

	raw := tree.Get("proxies")
	if raw == nil {
		return fmt.Errorf("could not find proxies section in frpc.toml")
	}

	switch v := raw.(type) {
	case []*toml.Tree:
		if len(v) == 0 {
			return fmt.Errorf("proxies section in frpc.toml is empty")
		}
		for _, proxy := range v {
			if proxy == nil {
				continue
			}
			metaRaw := proxy.Get("metadatas")
			var meta *toml.Tree
			if metaRaw == nil {
				m, err := toml.TreeFromMap(map[string]interface{}{})
				if err != nil {
					return fmt.Errorf("create metadatas table: %w", err)
				}
				meta = m
				proxy.Set("metadatas", meta)
			} else {
				mTree, ok := metaRaw.(*toml.Tree)
				if !ok {
					return fmt.Errorf("metadatas in frpc.toml is not a table")
				}
				meta = mTree
			}
			meta.Set("friendlyName", name)
		}
	default:
		return fmt.Errorf("unexpected proxies type in frpc.toml")
	}

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(tree); err != nil {
		return fmt.Errorf("encode frpc.toml: %w", err)
	}

	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		return err
	}

	return nil
}

func validateFriendlyName(name string) string {
	if strings.ContainsRune(name, '"') {
		return "name must not contain double quotes."
	}
	for _, r := range name {
		if r == '\n' || r == '\r' {
			return "name must not contain newlines."
		}
		if r < 0x20 {
			return "name must not contain control characters."
		}
	}
	return ""
}
