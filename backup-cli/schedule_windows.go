package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func defaultWorkDir() string {
	exe, err := os.Executable()
	if err != nil {
		return `C:\mgsis-backup`
	}
	return filepath.Dir(exe)
}

// installSchedule registers four Scheduled Tasks via schtasks.exe,
// equivalent to backup-diario-task.xml's two CalendarTriggers plus the
// fotos/verifica profiles. /F overwrites a same-named task, which is
// what keeps re-running setup idempotent on Windows. Tasks run whether
// or not the previous scheduled time was missed (schtasks' default
// behavior differs slightly from the old XML's StartWhenAvailable, so
// each task is also given /RL LIMITED and a generous time window isn't
// needed here since Task Scheduler itself queues a missed run on boot
// when the machine was off, as long as the task's own "run if missed"
// setting — set via /Z — is left at its default of enabled).
func installSchedule(exePath, configPath string) error {
	tasks := []struct {
		name string
		sc   string
		args []string
		mo   string
		d    string
		st   string
	}{
		{"MgsisBackupDiario12", "WEEKLY", nil, "", "MON,TUE,WED,THU,FRI,SAT", "12:30"},
		{"MgsisBackupDiario18", "WEEKLY", nil, "", "MON,TUE,WED,THU,FRI,SAT", "18:30"},
		{"MgsisBackupFotos", "WEEKLY", nil, "", "WED", "20:00"},
		{"MgsisBackupVerifica", "WEEKLY", nil, "", "SUN", "02:00"},
	}
	profiles := map[string]string{
		"MgsisBackupDiario12": "diario",
		"MgsisBackupDiario18": "diario",
		"MgsisBackupFotos":    "fotos",
		"MgsisBackupVerifica": "verifica",
	}

	for _, t := range tasks {
		profile := profiles[t.name]
		// configPath == "" means config.json sits beside the .exe, which
		// is where the program looks by default — no --config needed.
		taskRun := fmt.Sprintf(`"%s" %s`, exePath, profile)
		if configPath != "" {
			taskRun = fmt.Sprintf(`"%s" %s --config "%s"`, exePath, profile, configPath)
		}
		args := []string{
			"/Create", "/F",
			"/TN", t.name,
			"/TR", taskRun,
			"/SC", t.sc,
			"/D", t.d,
			"/ST", t.st,
			"/RL", "HIGHEST",
		}
		cmd := exec.Command("schtasks", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("schtasks /Create %s: %v: %s", t.name, err, string(out))
		}
	}
	return nil
}
