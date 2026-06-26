package config

import (
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Workdir  string         `toml:"workdir"`
	Port     int            `toml:"port"`
	HITL     HITLConfig     `toml:"hitl"`
	Obsidian ObsidianConfig `toml:"obsidian"`
	Limits   LimitsConfig   `toml:"limits"`
	HMAC     HMACConfig     `toml:"hmac"`
}

type HITLConfig struct {
	Timeout         Duration `toml:"timeout"`
	AutoReject      bool     `toml:"auto_reject"`
	RequireApproval []string `toml:"require_approval"`
}

type ObsidianConfig struct {
	VaultPath string `toml:"vault_path"`
	File      string `toml:"file"`
	Title     string `toml:"title"`
	Category  string `toml:"category"`
	WriteMode string `toml:"write_mode"`
}

type LimitsConfig struct {
	MaxOutputBytes int      `toml:"max_output_bytes"`
	MaxTimeout     Duration `toml:"max_timeout"`
}

type HMACConfig struct {
	Enabled bool   `toml:"enabled"`
	Key     string `toml:"key"`
}

type Duration struct{ time.Duration }

func (d *Duration) UnmarshalText(text []byte) (err error) {
	d.Duration, err = time.ParseDuration(string(text))
	return
}

func Load(path string) (*Config, error) {
	cfg := defaults()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		cfg.Workdir = mustAbs(".")
		return cfg, nil
	}

	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, err
	}

	cfg.Workdir = mustAbs(cfg.Workdir)
	return cfg, nil
}

func defaults() *Config {
	return &Config{
		Workdir: ".",
		Port:    8080,
		HITL: HITLConfig{
			Timeout:         Duration{30 * time.Second},
			AutoReject:      true,
			RequireApproval: []string{"write", "patch"},
		},
		Obsidian: ObsidianConfig{
			VaultPath: "/home/patatoid/Documents/Obsidian Vault/",
			File:      "DevLog_Copilot.md",
			Title:     "Analyse & Correction de bug",
			Category:  "Fix",
			WriteMode: "append",
		},
		Limits: LimitsConfig{
			MaxOutputBytes: 65536,
			MaxTimeout:     Duration{60 * time.Second},
		},
	}
}

func mustAbs(p string) string {
	a, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return a
}
