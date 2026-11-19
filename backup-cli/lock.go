package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// acquireLock is a simple cross-platform (Linux + Windows) run-once guard:
// it writes the current PID to a lock file, refusing to proceed if a
// recent lock file already exists. This mirrors the flock-based guard in
// the old backup_uni.sh (Linux) and the file-based guard used in the
// PowerShell version (Windows), unified into one portable mechanism so
// the same code compiles for both targets without OS-specific syscalls.
func acquireLock(workDir, name string, staleAfter time.Duration) (release func(), err error) {
	lockPath := filepath.Join(workDir, fmt.Sprintf(".%s.lock", name))

	if info, statErr := os.Stat(lockPath); statErr == nil {
		if time.Since(info.ModTime()) < staleAfter {
			return nil, fmt.Errorf("outra execução de %q já está em andamento (lock em %s, modificado há %s)", name, lockPath, time.Since(info.ModTime()).Round(time.Second))
		}
		// Lock is stale (older than staleAfter) — a previous run likely
		// crashed without cleaning up. Reclaim it rather than block forever.
		_ = os.Remove(lockPath)
	}

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("outra execução de %q já está em andamento (lock em %s)", name, lockPath)
		}
		return nil, fmt.Errorf("não consegui criar o arquivo de lock %s: %w", lockPath, err)
	}
	fmt.Fprintf(f, "%d\n", os.Getpid())
	f.Close()

	return func() {
		_ = os.Remove(lockPath)
	}, nil
}
