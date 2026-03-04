package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server        string        `yaml:"server"`
	Insecure      bool          `yaml:"insecure"`
	Timeout       time.Duration `yaml:"timeout"`
	OutputDir     string        `yaml:"output-dir"`
	Progress      bool          `yaml:"progress"`
	History       bool          `yaml:"history"`
	HideFilenames bool          `yaml:"hide-filenames"`
}

func DefaultConfig() *Config {
	return &Config{
		Server:        "",
		Insecure:      false,
		Timeout:       10 * time.Minute,
		OutputDir:     ".",
		Progress:      true,
		History:       true,
		HideFilenames: false,
	}
}

func ConfigPath() (string, error) {
	var configDir string

	switch runtime.GOOS {
	case "windows":
		configDir = os.Getenv("APPDATA")
		if configDir == "" {
			return "", errors.New("APPDATA environment variable not set")
		}
		configDir = filepath.Join(configDir, "xfer")
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		configDir = filepath.Join(home, "Library", "Application Support", "xfer")
	default:
		configDir = os.Getenv("XDG_CONFIG_HOME")
		if configDir == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			configDir = filepath.Join(home, ".config")
		}
		configDir = filepath.Join(configDir, "xfer")
	}

	return filepath.Join(configDir, "config.yaml"), nil
}

func Load() (*Config, error) {
	cfg := DefaultConfig()

	path, err := ConfigPath()
	if err != nil {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	cfg.applyEnvOverrides()

	return cfg, nil
}

func (c *Config) applyEnvOverrides() {
	if v := os.Getenv("XFER_SERVER"); v != "" {
		c.Server = v
	}
	if v := os.Getenv("XFER_INSECURE"); v != "" {
		c.Insecure = v == "true" || v == "1"
	}
	if v := os.Getenv("XFER_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			c.Timeout = d
		}
	}
	if v := os.Getenv("XFER_OUTPUT_DIR"); v != "" {
		c.OutputDir = v
	}
	if v := os.Getenv("XFER_PROGRESS"); v != "" {
		c.Progress = v == "true" || v == "1"
	}
	if v := os.Getenv("XFER_HISTORY"); v != "" {
		c.History = v == "true" || v == "1"
	}
	if v := os.Getenv("XFER_HIDE_FILENAMES"); v != "" {
		c.HideFilenames = v == "true" || v == "1"
	}
}

func (c *Config) Save() error {
	path, err := ConfigPath()
	if err != nil {
		return fmt.Errorf("failed to determine config path: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to serialize config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

func (c *Config) Get(key string) (string, error) {
	switch strings.ToLower(key) {
	case "server":
		return c.Server, nil
	case "insecure":
		return strconv.FormatBool(c.Insecure), nil
	case "timeout":
		return c.Timeout.String(), nil
	case "output-dir":
		return c.OutputDir, nil
	case "progress":
		return strconv.FormatBool(c.Progress), nil
	case "history":
		return strconv.FormatBool(c.History), nil
	case "hide-filenames":
		return strconv.FormatBool(c.HideFilenames), nil
	default:
		return "", fmt.Errorf("unknown configuration key: %s", key)
	}
}

func (c *Config) Set(key, value string) error {
	switch strings.ToLower(key) {
	case "server":
		c.Server = value
	case "insecure":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid boolean value: %s", value)
		}
		c.Insecure = b
	case "timeout":
		d, err := time.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("invalid duration value: %s", value)
		}
		c.Timeout = d
	case "output-dir":
		c.OutputDir = value
	case "progress":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid boolean value: %s", value)
		}
		c.Progress = b
	case "history":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid boolean value: %s", value)
		}
		c.History = b
	case "hide-filenames":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid boolean value: %s", value)
		}
		c.HideFilenames = b
	default:
		return fmt.Errorf("unknown configuration key: %s", key)
	}
	return nil
}

func ListKeys() map[string]string {
	return map[string]string{
		"server":         "Server URL (e.g., https://xfer.example.com)",
		"insecure":       "Skip TLS verification (true/false)",
		"timeout":        "Transfer timeout (e.g., 10m, 1h)",
		"output-dir":     "Default directory for received files",
		"progress":       "Show detailed progress (true/false)",
		"history":        "Track transfer history (true/false)",
		"hide-filenames": "Hide filenames in history (true/false)",
	}
}

func (c *Config) Validate() error {
	if c.Server == "" {
		return errors.New("server URL is required. Set it with: xfer config set server <url>")
	}
	return nil
}
