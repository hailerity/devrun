package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LoadRegistry reads services.yaml. Returns an empty registry if the file does not exist.
func LoadRegistry(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Registry{Version: "1", Services: make(map[string]*ServiceConfig)}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read registry: %w", err)
	}
	var reg Registry
	if err := yaml.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("parse registry: %w", err)
	}
	if reg.Services == nil {
		reg.Services = make(map[string]*ServiceConfig)
	}
	return &reg, nil
}

// SaveRegistry writes the registry to path, creating parent directories as needed.
func SaveRegistry(path string, reg *Registry) error {
	data, err := yaml.Marshal(reg)
	if err != nil {
		return fmt.Errorf("marshal registry: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("mkdir registry dir: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write registry: %w", err)
	}
	return nil
}
