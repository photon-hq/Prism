package host

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"prism/internal/infra/config"
	"prism/internal/infra/deps"
	infrahost "prism/internal/infra/host"
	"prism/internal/infra/macos"
	"prism/internal/infra/state"
)

// Initializer coordinates host management flows.
// It handles the host lifecycle at a high level:
// - running read-only environment checks (preflight + deps + state + config)
// - provisioning initial Prism users and host services
// - adding or removing users on an initialized host and reporting service status.
type Initializer struct {
	ConfigPath string
	StatePath  string

	loadConfig func(string) (config.Config, error)
	loadState  func(string) (state.State, error)
	saveState  func(string, state.State) error

	preflight  func(context.Context) (macos.PreflightResult, error)
	ensureDeps func(context.Context) (deps.Result, error)

	provisionUsers func(ctx context.Context, cfg config.Config, st state.State, userCount int, outputDir, prismPath string) (state.State, string, error)
	addUsers       func(ctx context.Context, cfg config.Config, st state.State, userCount int, outputDir, prismPath string) (state.State, string, error)
	removeUser     func(ctx context.Context, cfg config.Config, st state.State, username, outputDir string) (state.State, error)

	checkServices        func(ctx context.Context, cfg config.Config, st state.State) ([]infrahost.UserServiceStatus, error)
	ensureAutobootDaemon func(ctx context.Context, prismPath, workingDir string) error
}

// ServiceStatus is an alias for infrahost.UserServiceStatus.
type ServiceStatus = infrahost.UserServiceStatus

// Result describes the outcome of the host check flow.
type Result struct {
	AlreadyInitialized bool
	Preflight          macos.PreflightResult
	Deps               deps.Result
}

// ProvisionResult describes the outcome of user provisioning.
type ProvisionResult struct {
	State       state.State
	SecretsPath string
}

// NewInitializer constructs an Initializer with default implementations.
func NewInitializer(configPath, statePath string) *Initializer {
	return &Initializer{
		ConfigPath:           configPath,
		StatePath:            statePath,
		loadConfig:           config.Load,
		loadState:            state.Load,
		saveState:            state.Save,
		preflight:            macos.Preflight,
		ensureDeps:           deps.Ensure,
		provisionUsers:       infrahost.ProvisionUsers,
		addUsers:             infrahost.AddUsers,
		removeUser:           infrahost.RemoveUser,
		checkServices:        infrahost.CheckUserServices,
		ensureAutobootDaemon: infrahost.EnsureHostAutobootDaemon,
	}
}

// Run performs a read-only environment check (preflight + deps).
func (i *Initializer) Run(ctx context.Context) (Result, error) {
	if err := i.validate(); err != nil {
		return Result{}, err
	}

	pfRes, err := i.preflight(ctx)
	if err != nil {
		return Result{Preflight: pfRes}, fmt.Errorf("preflight: %w", err)
	}

	depsRes, err := i.ensureDeps(ctx)
	if err != nil {
		return Result{Preflight: pfRes, Deps: depsRes}, fmt.Errorf("deps: %w", err)
	}

	st, err := i.loadState(i.StatePath)
	if err != nil {
		return Result{Preflight: pfRes, Deps: depsRes}, fmt.Errorf("load state: %w", err)
	}

	if st.Initialized || len(st.Users) > 0 {
		return Result{AlreadyInitialized: true, Preflight: pfRes, Deps: depsRes}, nil
	}

	if _, err := i.loadConfig(i.ConfigPath); err != nil {
		return Result{Preflight: pfRes, Deps: depsRes}, fmt.Errorf("load config: %w", err)
	}

	return Result{AlreadyInitialized: false, Preflight: pfRes, Deps: depsRes}, nil
}

// Provisioning flows.
// Provision creates users and prepares per-user service bundles.
func (i *Initializer) Provision(ctx context.Context, userCount int, prismPath string) (ProvisionResult, error) {
	if err := i.validate(); err != nil {
		return ProvisionResult{}, err
	}

	if userCount <= 0 {
		return ProvisionResult{}, errors.New("userCount must be positive")
	}

	cfg, err := i.loadConfig(i.ConfigPath)
	if err != nil {
		return ProvisionResult{}, fmt.Errorf("load config: %w", err)
	}

	st, err := i.loadState(i.StatePath)
	if err != nil {
		return ProvisionResult{}, fmt.Errorf("load state: %w", err)
	}

	outputDir := filepath.Dir(i.StatePath)
	newState, secretsPath, err := i.provisionUsers(ctx, cfg, st, userCount, outputDir, prismPath)
	if err != nil {
		return ProvisionResult{}, fmt.Errorf("provision users: %w", err)
	}

	if err := i.saveState(i.StatePath, newState); err != nil {
		return ProvisionResult{}, fmt.Errorf("save state: %w", err)
	}

	// WorkingDir = directory containing prism binary and .env file
	if err := i.ensureAutobootDaemon(ctx, prismPath, filepath.Dir(prismPath)); err != nil {
		return ProvisionResult{}, fmt.Errorf("ensure host autoboot daemon: %w", err)
	}

	return ProvisionResult{State: newState, SecretsPath: secretsPath}, nil
}

func (i *Initializer) validate() error {
	if i == nil {
		return errors.New("initializer is nil")
	}

	if i.ConfigPath == "" {
		return errors.New("config path is empty")
	}

	if i.StatePath == "" {
		return errors.New("state path is empty")
	}

	return nil
}

// User management flows.
// UserServiceStatuses returns runtime status for each Prism-managed user.
func (i *Initializer) UserServiceStatuses(ctx context.Context) ([]ServiceStatus, error) {
	if err := i.validate(); err != nil {
		return nil, err
	}

	cfg, err := i.loadConfig(i.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	st, err := i.loadState(i.StatePath)
	if err != nil {
		return nil, fmt.Errorf("load state: %w", err)
	}

	statuses, err := i.checkServices(ctx, cfg, st)
	if err != nil {
		return nil, fmt.Errorf("check services: %w", err)
	}

	return statuses, nil
}

// RemoveUser deletes a Prism-managed user and updates state.
func (i *Initializer) RemoveUser(ctx context.Context, username string) (state.State, error) {
	if err := i.validate(); err != nil {
		return state.State{}, err
	}

	if strings.TrimSpace(username) == "" {
		return state.State{}, errors.New("username is empty")
	}

	cfg, err := i.loadConfig(i.ConfigPath)
	if err != nil {
		return state.State{}, fmt.Errorf("load config: %w", err)
	}

	st, err := i.loadState(i.StatePath)
	if err != nil {
		return state.State{}, fmt.Errorf("load state: %w", err)
	}

	outputDir := filepath.Dir(i.StatePath)
	newState, err := i.removeUser(ctx, cfg, st, username, outputDir)
	if err != nil {
		return state.State{}, fmt.Errorf("remove user: %w", err)
	}

	if err := i.saveState(i.StatePath, newState); err != nil {
		return state.State{}, fmt.Errorf("save state: %w", err)
	}

	return newState, nil
}

// AddUsers appends additional users on an already-initialized host.
func (i *Initializer) AddUsers(ctx context.Context, userCount int, prismPath string) (ProvisionResult, error) {
	if err := i.validate(); err != nil {
		return ProvisionResult{}, err
	}

	if userCount <= 0 {
		return ProvisionResult{}, errors.New("userCount must be positive")
	}

	cfg, err := i.loadConfig(i.ConfigPath)
	if err != nil {
		return ProvisionResult{}, fmt.Errorf("load config: %w", err)
	}

	st, err := i.loadState(i.StatePath)
	if err != nil {
		return ProvisionResult{}, fmt.Errorf("load state: %w", err)
	}

	outputDir := filepath.Dir(i.StatePath)
	newState, secretsPath, err := i.addUsers(ctx, cfg, st, userCount, outputDir, prismPath)
	if err != nil {
		return ProvisionResult{}, fmt.Errorf("add users: %w", err)
	}

	if err := i.saveState(i.StatePath, newState); err != nil {
		return ProvisionResult{}, fmt.Errorf("save state: %w", err)
	}

	return ProvisionResult{State: newState, SecretsPath: secretsPath}, nil
}

func (i *Initializer) UpdateUserCode(ctx context.Context) (ProvisionResult, error) {
	if err := i.validate(); err != nil {
		return ProvisionResult{}, err
	}

	cfg, err := i.loadConfig(i.ConfigPath)
	if err != nil {
		return ProvisionResult{}, fmt.Errorf("load config: %w", err)
	}

	st, err := i.loadState(i.StatePath)
	if err != nil {
		return ProvisionResult{}, fmt.Errorf("load state: %w", err)
	}

	outputDir := filepath.Dir(i.StatePath)
	newState, err := infrahost.UpdateUserCode(ctx, cfg, st, outputDir)
	if err != nil {
		return ProvisionResult{}, fmt.Errorf("update user code: %w", err)
	}

	if err := i.saveState(i.StatePath, newState); err != nil {
		return ProvisionResult{}, fmt.Errorf("save state: %w", err)
	}

	return ProvisionResult{State: newState}, nil
}
