//go:build darwin

package host

import (
	"log"

	"prism/internal/infra/state"
)

// RunAutoboot ensures all per-user LaunchDaemons are running at system startup.
// Called by the host-autoboot LaunchDaemon. Services should already be running
// via RunAtLoad; this is a safety net to ensure proper bootstrapping.
func RunAutoboot(statePath string) {
	st, err := state.Load(statePath)
	if err != nil {
		log.Printf("[host-autoboot] load state: %v", err)
		return
	}

	for _, u := range st.Users {
		if err := BootstrapUserLaunchDaemons(u.Name); err != nil {
			log.Printf("[host-autoboot] %s: %v", u.Name, err)
		}
	}
}
