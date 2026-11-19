package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// defaultMasterEnvPath mirrors exactly what setup_b2_bucket.sh used:
// ~/.config/b2-auto/master.env. Using the identical path means a server
// that already ran the old shell-based installer has this file sitting
// there already — so this tool picks it up and never asks for the
// master key again, same as before. os.UserConfigDir() resolves to
// $XDG_CONFIG_HOME or $HOME/.config on Linux (matching the old script)
// and %AppData% on Windows.
func defaultMasterEnvPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "b2-auto", "master.env")
}

// loadMasterEnv reads MASTER_KEY_ID / MASTER_APP_KEY from a file in the
// same simple "KEY=VALUE" shape the old script wrote. The master key
// never needs to be typed into this chat or any remote session once
// it's saved here — it stays local to the machine running the install.
func loadMasterEnv(path string) (keyID, appKey string, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		switch strings.TrimSpace(parts[0]) {
		case "MASTER_KEY_ID":
			keyID = strings.TrimSpace(parts[1])
		case "MASTER_APP_KEY":
			appKey = strings.TrimSpace(parts[1])
		}
	}
	return keyID, appKey, keyID != "" && appKey != ""
}

// saveMasterEnv writes the master credentials to disk so the next
// install on this machine (or the next client provisioned from it)
// never needs them typed in again. Directory 0700, file 0600 — same
// permissions the old script used.
func saveMasterEnv(path, keyID, appKey string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("não consegui criar %s: %w", dir, err)
	}
	content := fmt.Sprintf("MASTER_KEY_ID=%s\nMASTER_APP_KEY=%s\n", keyID, appKey)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return fmt.Errorf("não consegui salvar %s: %w", path, err)
	}
	return nil
}
