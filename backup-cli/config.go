package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DatabaseEntry is one database backed up by this server. Each database
// is one client (RUC) and has its own B2 bucket and its own restricted
// application key, which can only reach that bucket.
type DatabaseEntry struct {
	ClientID   string `json:"client_id"`
	PGDatabase string `json:"pg_database"`
	B2KeyID    string `json:"b2_key_id"`
	B2AppKey   string `json:"b2_application_key"`
	B2Bucket   string `json:"b2_bucket"`
}

// Config replaces the PREENCHA_* placeholders that used to live inside
// backup_uni.sh / backup_verifica.sh. It is a plain JSON file sitting next
// to the executable (or pointed to with --config), so it is just as easy
// to hand-edit as the old shell scripts were — but editing it can never
// break shell quoting or introduce a syntax error that stops the backup.
//
// A server may host several databases (up to 3 today). Everything that
// is the same for all of them on one server — PostgreSQL user, host,
// port, password, work dir, retention — sits at the top level; what
// changes per database lives in Databases ("bancos" in the JSON).
//
// ClientID / PGDatabase / B2* at the top level serve two purposes:
//   - reading an older single-database config.json, which had them at
//     the top level (normalize() moves them into Databases);
//   - the per-database copies returned by Targets(), which the backup,
//     verification and B2 code use as "the database being processed".
//
// They are never written back to disk (omitempty + normalize clears them).
type Config struct {
	ClientID   string `json:"client_id,omitempty"`
	PGDatabase string `json:"pg_database,omitempty"`
	PGUser     string `json:"pg_user"`
	PGHost     string `json:"pg_host"`
	PGPort     string `json:"pg_port"`
	PGPassword string `json:"pg_password"`

	B2KeyID  string `json:"b2_key_id,omitempty"`
	B2AppKey string `json:"b2_application_key,omitempty"`
	B2Bucket string `json:"b2_bucket,omitempty"`
	B2Prefix string `json:"b2_prefix"`

	WorkDir string `json:"work_dir"`

	RetentionDumpDays int `json:"retention_dump_days"`
	RetentionZipDays  int `json:"retention_zip_days"`

	ExcludeFotos bool `json:"exclude_fotos_from_diario"`

	// Physical backup (profile "fisico"): a .tar.gz of the whole
	// PostgreSQL data directory — so it covers every database on the
	// server at once and is uploaded once, to one bucket. Only checked
	// when the fisico profile runs (see ValidatePhysical); an existing
	// config.json without these fields keeps working for everything else.
	//
	// PhysicalMethod: "legacy_copy" (default, as in backup_uni.sh) or
	// "pg_basebackup". PhysicalSourceDir: the data directory, e.g.
	// /var/lib/postgresql/11/main (required for legacy_copy).
	// PhysicalClientID: which database's bucket receives it (default:
	// the first one in "bancos").
	PhysicalMethod    string `json:"physical_method,omitempty"`
	PhysicalSourceDir string `json:"physical_source_dir,omitempty"`
	PhysicalClientID  string `json:"physical_client_id,omitempty"`

	Databases []DatabaseEntry `json:"bancos,omitempty"`
}

const (
	physicalLegacyCopy   = "legacy_copy"
	physicalPgBasebackup = "pg_basebackup"
)

// physicalMethod returns the configured method, defaulting to the
// historical legacy_copy when the field is absent.
func (c Config) physicalMethod() string {
	if c.PhysicalMethod == "" {
		return physicalLegacyCopy
	}
	return c.PhysicalMethod
}

// errInvalidPhysicalMethod is returned by ValidatePhysical for an
// unknown method, so the caller can exit with code 64 like the script.
var errInvalidPhysicalMethod = fmt.Errorf("physical_method inválido")

// ValidatePhysical checks only what the fisico profile needs, mirroring
// backup_uni.sh, where PHYSICAL_* were required only for --fisico.
func (c Config) ValidatePhysical() error {
	switch c.physicalMethod() {
	case physicalLegacyCopy:
		if c.PhysicalSourceDir == "" {
			return fmt.Errorf("physical_source_dir é obrigatório com physical_method=legacy_copy (ex.: /var/lib/postgresql/11/main)")
		}
		info, err := os.Stat(c.PhysicalSourceDir)
		if err != nil {
			return fmt.Errorf("physical_source_dir %s não acessível: %w", c.PhysicalSourceDir, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("physical_source_dir %s não é um diretório", c.PhysicalSourceDir)
		}
	case physicalPgBasebackup:
		// Pulls over the replication protocol; no local path needed.
	default:
		return fmt.Errorf("%w: %q (use %s ou %s)", errInvalidPhysicalMethod, c.PhysicalMethod, physicalLegacyCopy, physicalPgBasebackup)
	}
	return nil
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
	if len(cfg.Databases) > 0 && cfg.hasLegacyFields() {
		return cfg, fmt.Errorf("configuração em %s mistura o formato antigo (client_id/pg_database/b2_* no topo) com a lista \"bancos\" — deixe só a lista \"bancos\"", path)
	}
	cfg.normalize()
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
	cfg.normalize()
	return cfg, true
}

func (c Config) hasLegacyFields() bool {
	return c.ClientID != "" || c.PGDatabase != "" || c.B2KeyID != "" || c.B2AppKey != "" || c.B2Bucket != ""
}

// normalize turns an older single-database config.json into a one-item
// Databases list, so the rest of the program only deals with the list.
func (c *Config) normalize() {
	if len(c.Databases) == 0 && c.hasLegacyFields() {
		c.Databases = []DatabaseEntry{{
			ClientID:   c.ClientID,
			PGDatabase: c.PGDatabase,
			B2KeyID:    c.B2KeyID,
			B2AppKey:   c.B2AppKey,
			B2Bucket:   c.B2Bucket,
		}}
	}
	c.ClientID, c.PGDatabase, c.B2KeyID, c.B2AppKey, c.B2Bucket = "", "", "", "", ""
}

// Targets returns one Config per database, each with the shared server
// settings plus that database's ClientID / PGDatabase / B2 credentials
// filled in at the top level.
func (c Config) Targets() []Config {
	out := make([]Config, 0, len(c.Databases))
	for _, d := range c.Databases {
		t := c
		t.Databases = nil
		t.ClientID = d.ClientID
		t.PGDatabase = d.PGDatabase
		t.B2KeyID = d.B2KeyID
		t.B2AppKey = d.B2AppKey
		t.B2Bucket = d.B2Bucket
		out = append(out, t)
	}
	return out
}

func (c Config) Validate() error {
	if len(c.Databases) == 0 {
		return fmt.Errorf("configuração incompleta: nenhum banco em \"bancos\"")
	}
	var problems []string
	seenClient := map[string]int{}
	seenDB := map[string]int{}
	seenBucket := map[string]int{}
	for i, d := range c.Databases {
		n := i + 1
		var missing []string
		if d.ClientID == "" {
			missing = append(missing, "client_id")
		}
		if d.PGDatabase == "" {
			missing = append(missing, "pg_database")
		}
		if d.B2KeyID == "" {
			missing = append(missing, "b2_key_id")
		}
		if d.B2AppKey == "" {
			missing = append(missing, "b2_application_key")
		}
		if d.B2Bucket == "" {
			missing = append(missing, "b2_bucket")
		}
		if len(missing) > 0 {
			problems = append(problems, fmt.Sprintf("banco %d faltando %v", n, missing))
		}
		// client_id prefixes every dump/result file in the shared work dir,
		// so two entries with the same one would overwrite each other.
		if prev, ok := seenClient[d.ClientID]; ok && d.ClientID != "" {
			problems = append(problems, fmt.Sprintf("client_id %q repetido nos bancos %d e %d", d.ClientID, prev, n))
		}
		if prev, ok := seenDB[d.PGDatabase]; ok && d.PGDatabase != "" {
			problems = append(problems, fmt.Sprintf("pg_database %q repetido nos bancos %d e %d", d.PGDatabase, prev, n))
		}
		if prev, ok := seenBucket[d.B2Bucket]; ok && d.B2Bucket != "" {
			problems = append(problems, fmt.Sprintf("b2_bucket %q repetido nos bancos %d e %d", d.B2Bucket, prev, n))
		}
		seenClient[d.ClientID] = n
		seenDB[d.PGDatabase] = n
		seenBucket[d.B2Bucket] = n
	}
	if c.PhysicalClientID != "" {
		if _, ok := seenClient[c.PhysicalClientID]; !ok {
			problems = append(problems, fmt.Sprintf("physical_client_id %q não é o client_id de nenhum banco", c.PhysicalClientID))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("configuração inválida: %s", strings.Join(problems, "; "))
	}
	return nil
}

func SaveConfig(path string, cfg Config) error {
	cfg.normalize()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	// 0600: contains PGPassword and the B2 application keys.
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
