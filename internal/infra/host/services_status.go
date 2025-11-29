//go:build darwin

package host

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"prism/internal/infra/config"
	"prism/internal/infra/state"
)

// UserServiceStatus describes the runtime status of a Prism-managed user.
type UserServiceStatus struct {
	Name          string `json:"name"`
	Port          int    `json:"port"`
	Subdomain     string `json:"subdomain"`
	ServiceDirOK  bool   `json:"service_dir_ok"`
	PortListening bool   `json:"port_listening"`
	Detail        string `json:"detail"`
}

// CheckUserServices reports runtime status for each Prism-managed user.
func CheckUserServices(ctx context.Context, cfg config.Config, st state.State) ([]UserServiceStatus, error) {
	statuses := make([]UserServiceStatus, 0, len(st.Users))
	for _, u := range st.Users {
		stItem := UserServiceStatus{
			Name:      u.Name,
			Port:      u.Port,
			Subdomain: u.Subdomain,
		}

		var details []string

		homeDir := filepath.Join("/Users", u.Name)
		serviceDir := filepath.Join(homeDir, "services", "imsg")
		if fi, err := os.Stat(serviceDir); err == nil && fi.IsDir() {
			stItem.ServiceDirOK = true
		} else {
			if err != nil {
				details = append(details, fmt.Sprintf("service dir missing or unreadable: %v", err))
			} else {
				details = append(details, "service dir is not a directory")
			}
		}

		if u.Port > 0 {
			addr := fmt.Sprintf("127.0.0.1:%d", u.Port)
			dialer := &net.Dialer{Timeout: 500 * time.Millisecond}
			conn, err := dialer.DialContext(ctx, "tcp", addr)
			if err == nil {
				stItem.PortListening = true
				_ = conn.Close()
			} else {
				details = append(details, fmt.Sprintf("no listener on %s: %v", addr, err))
			}
		}

		if len(details) > 0 {
			stItem.Detail = strings.Join(details, "; ")
		}

		statuses = append(statuses, stItem)
	}
	return statuses, nil
}
