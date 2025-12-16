package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// Config represents the static configuration loaded from prism.json.
type Config struct {
	Globals Globals `json:"globals"`
}

type Globals struct {
	MachineID       string        `json:"machine_id"`
	DefaultPassword string        `json:"default_password"`
	FRPC            FRPCConfig    `json:"frpc"`
	DomainSuffix    string        `json:"domain_suffix"`
	Service         ServiceConfig `json:"service"`
	Nexus           NexusConfig   `json:"nexus"`
}

type FRPCConfig struct {
	ServerAddr string `json:"server_addr"`
	ServerPort int    `json:"server_port"`
}

type ServiceConfig struct {
	ArchiveURL string `json:"archive_url"`
	StartPort  int    `json:"start_port"`
}

type NexusConfig struct {
	BaseURL string `json:"base_url"`
}

// Load reads and validates configuration from the given path.
func Load(path string) (Config, error) {
	if path == "" {
		return Config{}, errors.New("config path is empty")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (c Config) Validate() error {
	if c.Globals.MachineID == "" {
		return errors.New("globals.machine_id is required")
	}

	if err := c.Globals.FRPC.validate(); err != nil {
		return err
	}

	if c.Globals.DomainSuffix == "" {
		return errors.New("globals.domain_suffix is required")
	}

	if err := c.Globals.Service.validate(); err != nil {
		return err
	}

	if err := c.Globals.Nexus.validate(); err != nil {
		return err
	}

	return nil
}

func (c FRPCConfig) validate() error {
	if c.ServerAddr == "" {
		return errors.New("globals.frpc.server_addr is required")
	}

	if c.ServerPort <= 0 || c.ServerPort > 65535 {
		return errors.New("globals.frpc.server_port must be between 1 and 65535")
	}

	return nil
}

func (s ServiceConfig) validate() error {
	if s.ArchiveURL == "" {
		return errors.New("globals.service.archive_url is required")
	}

	if s.StartPort <= 0 || s.StartPort > 65535 {
		return errors.New("globals.service.start_port must be between 1 and 65535")
	}

	return nil
}

func (n NexusConfig) validate() error {
	if n.BaseURL == "" {
		return errors.New("globals.nexus.base_url is required")
	}

	return nil
}
