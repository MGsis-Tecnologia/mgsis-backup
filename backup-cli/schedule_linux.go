package main

import (
	"fmt"
	"os/exec"
	"strings"
)

// scheduleMarker tags every line this program adds to crontab, so
// re-running setup on the same machine is idempotent: old mgsis-backup
// lines are replaced rather than duplicated.
const scheduleMarker = "# mgsis-backup"

func defaultWorkDir() string {
	return "/dados/backup"
}

// installSchedule installs the cron entries equivalent to the old
// crontab.template.txt, pointed at this binary and its config file
// instead of backup_uni.sh. It reads the current crontab, strips any
// previous mgsis-backup block, and appends the new one — so running
// `setup` again (e.g. after regenerating config.json) doesn't pile up
// duplicate entries.
func installSchedule(exePath, configPath string) error {
	existing, _ := exec.Command("crontab", "-l").Output() // non-zero exit if no crontab yet is fine

	var kept []string
	for _, line := range strings.Split(string(existing), "\n") {
		if strings.Contains(line, scheduleMarker) {
			continue
		}
		if line != "" {
			kept = append(kept, line)
		}
	}

	// configPath == "" means config.json sits beside the executable, which
	// is where the program looks by default — no --config needed in cron.
	bin := shellQuote(exePath)
	cfgFlag := ""
	if configPath != "" {
		cfgFlag = " --config " + shellQuote(configPath)
	}
	newLines := []string{
		fmt.Sprintf("30 12 * * 1-6 %s diario%s >/dev/null 2>&1 %s", bin, cfgFlag, scheduleMarker),
		fmt.Sprintf("30 18 * * 1-6 %s diario%s >/dev/null 2>&1 %s", bin, cfgFlag, scheduleMarker),
		fmt.Sprintf("0 20 * * 3 %s fotos%s >/dev/null 2>&1 %s", bin, cfgFlag, scheduleMarker),
		fmt.Sprintf("0 2 * * 0 %s verifica%s >/dev/null 2>&1 %s", bin, cfgFlag, scheduleMarker),
	}

	final := strings.Join(append(kept, newLines...), "\n") + "\n"

	cmd := exec.Command("crontab", "-")
	cmd.Stdin = strings.NewReader(final)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("crontab -: %v: %s", err, string(out))
	}
	return nil
}

// shellQuote leaves plain paths readable and single-quotes ones with
// spaces or special characters.
func shellQuote(s string) string {
	if strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/._-+:@", r))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
