package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config replaces the PREENCHA_* placeholders that used to live inside
// backup_uni.sh / backup_verifica.sh. It is a plain JSON file sitting next
// to the executable (or pointed to with --config), so it is just as easy
// to hand-edit as the old shell scripts were — but editing it can never
// break shell quoting or introduce a syntax error that stops the backup.
type Config struct {
	ClientID   string `json:"client_id"`
	PGDatabase string `json:"pg_database"`
	PGUser     string `json:"pg_user"`
	PGHost     string `json:"pg_host"`
	PGPort     string `json:"pg_port"`
	PGPassword string `json:"pg_password"`

	B2KeyID  string `json:"b2_key_id"`
	B2AppKey string `json:"b2_application_key"`
	B2Bucket string `json:"b2_bucket"`
	B2Prefix string `json:"b2_prefix"`

	WorkDir string `json:"work_dir"`

	RetentionDumpDays int `json:"retention_dump_days"`
	RetentionZipDays  int `json:"retention_zip_days"`

	ExcludeFotos bool `json:"exclude_fotos_from_diario"`
}

func DefaultConfig() Config {
	return Config{
		PGUser:            "postgres",
		PGHost:            "localhost",
		PGPort:            "5432",
		B2Prefix:          "backups",
		WorkDir:           "/dados/backup",
		RetentionDumpDays: 7,
		RetentionZipDays:  30,
		ExcludeFotos:      true,
	}
}

func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("não consegui ler o arquivo de configuração %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("configuração em %s não é um JSON válido: %w", path, err)
	}
	return cfg, cfg.Validate()
}

// LoadConfigLenient reads a config.json if it exists, without requiring
// it to be complete. Used by the interactive setup so a support user can
// drop in a half-filled config.json (e.g. already carrying the B2 keys)
// and only answer for what's still missing or needs changing.
func LoadConfigLenient(path string) (Config, bool) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, false
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return DefaultConfig(), false
	}
	return cfg, true
}

func (c Config) Validate() error {
	missing := []string{}
	if c.ClientID == "" {
		missing = append(missing, "client_id")
	}
	if c.PGDatabase == "" {
		missing = append(missing, "pg_database")
	}
	if c.B2KeyID == "" {
		missing = append(missing, "b2_key_id")
	}
	if c.B2AppKey == "" {
		missing = append(missing, "b2_application_key")
	}
	if c.B2Bucket == "" {
		missing = append(missing, "b2_bucket")
	}
	if len(missing) > 0 {
		return fmt.Errorf("configuração incompleta, faltando: %v", missing)
	}
	return nil
}

func SaveConfig(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	// 0600: contains PGPassword and the B2 application key.
	return os.WriteFile(path, data, 0600)
}

// defaultConfigPath returns config.json next to the running executable,
// matching the "everything the backup needs lives beside the .exe" model
// used by the catalog project.
func defaultConfigPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "config.json"
	}
	return filepath.Join(filepath.Dir(exe), "config.json")
}
