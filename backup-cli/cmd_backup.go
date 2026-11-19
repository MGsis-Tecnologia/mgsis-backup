package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"mgsis-backup/b2"
)

type profileSpec struct {
	name        string
	filePrefix  string // suffix appended to ClientID in the filename
	pgDumpArgs  []string
	b2Subfolder string
}

func profileFor(name, clientID string) (profileSpec, error) {
	switch name {
	case "diario":
		return profileSpec{
			name:        "diario",
			filePrefix:  clientID,
			pgDumpArgs:  []string{"--exclude-table=produto_foto", "--exclude-table=log_sistema"},
			b2Subfolder: "diario",
		}, nil
	case "fotos":
		return profileSpec{
			name:        "fotos",
			filePrefix:  clientID + "-IMG",
			pgDumpArgs:  []string{"--table=produto_foto"},
			b2Subfolder: "fotos",
		}, nil
	case "logs":
		return profileSpec{
			name:        "logs",
			filePrefix:  clientID + "-LOGS",
			pgDumpArgs:  []string{"--table=log_sistema"},
			b2Subfolder: "logs",
		}, nil
	default:
		return profileSpec{}, fmt.Errorf("perfil desconhecido: %s (use diario, fotos ou logs)", name)
	}
}

// runBackup runs one profile for every database in the config, one after
// the other (never in parallel: they share the same PostgreSQL server
// and upload link). A failure in one database is logged and the next one
// still runs; the exit code is non-zero if any of them failed.
func runBackup(cfg Config, profileName string, dryRun bool) int {
	if _, err := profileFor(profileName, ""); err != nil {
		fmt.Fprintln(os.Stderr, "Erro:", err)
		return 1
	}

	logger, err := NewLogger(cfg.WorkDir, profileName, fmt.Sprintf("backup-%s.log", profileName))
	if err != nil {
		fmt.Fprintln(os.Stderr, "Erro ao abrir log:", err)
		return 1
	}
	defer logger.Close()

	release, err := acquireLock(cfg.WorkDir, "backup-"+profileName, 6*time.Hour)
	if err != nil {
		logger.Warn("%s", err)
		return 75
	}
	defer release()

	targets := cfg.Targets()
	logger.Info("Iniciando backup no host %s; %d banco(s).", hostname(), len(targets))

	if err := os.MkdirAll(cfg.WorkDir, 0700); err != nil {
		logger.Error("Falha ao criar diretório de trabalho: %v", err)
		return 1
	}

	cleanupLocalRetention(cfg)

	var failed []string
	for i, t := range targets {
		spec, _ := profileFor(profileName, t.ClientID)
		logger.SetScope(profileName + ":" + t.ClientID)
		logger.Info("Banco %d/%d: %s (cliente %s, bucket %s).", i+1, len(targets), t.PGDatabase, t.ClientID, t.B2Bucket)
		if err := backupOne(t, spec, logger, dryRun); err != nil {
			logger.Error("%v", err)
			failed = append(failed, t.ClientID)
			continue
		}
		logger.Info("SUCESSO: backup do cliente %s concluído.", t.ClientID)
	}

	logger.SetScope(profileName)
	if len(failed) > 0 {
		logger.Error("Rotina concluída com FALHA em %d de %d banco(s): %v", len(failed), len(targets), failed)
		return 1
	}
	logger.Info("SUCESSO: rotina concluída para %d banco(s).", len(targets))
	return 0
}

// backupOne dumps, compresses, validates and uploads one database.
func backupOne(cfg Config, spec profileSpec, logger *Logger, dryRun bool) error {
	timestamp := time.Now().Format("02-01-06-15-04")
	dumpName := fmt.Sprintf("%s-%s.backup", spec.filePrefix, timestamp)
	dumpPath := filepath.Join(cfg.WorkDir, dumpName)
	archiveName := fmt.Sprintf("%s-%s.tar.gz", spec.filePrefix, timestamp)
	archivePath := filepath.Join(cfg.WorkDir, archiveName)

	logger.Info("Gerando dump PostgreSQL: %s", dumpName)
	if dryRun {
		logger.Info("DRY-RUN: pg_dump -h %s -p %s -U %s -d %s %v --file %s", cfg.PGHost, cfg.PGPort, cfg.PGUser, cfg.PGDatabase, spec.pgDumpArgs, dumpPath+".partial")
	} else {
		if err := runPgDump(cfg, spec.pgDumpArgs, dumpPath+".partial"); err != nil {
			_ = os.Remove(dumpPath + ".partial")
			return fmt.Errorf("falha no pg_dump: %w", err)
		}
		if err := os.Rename(dumpPath+".partial", dumpPath); err != nil {
			return fmt.Errorf("falha ao renomear dump: %w", err)
		}
	}

	logger.Info("Compactando em: %s", archiveName)
	if dryRun {
		logger.Info("DRY-RUN: compactar %s -> %s", dumpPath, archivePath)
	} else {
		if err := compressToTarGz(dumpPath, archivePath+".partial"); err != nil {
			_ = os.Remove(archivePath + ".partial")
			return fmt.Errorf("falha ao compactar: %w", err)
		}
		if err := os.Rename(archivePath+".partial", archivePath); err != nil {
			return fmt.Errorf("falha ao renomear arquivo compactado: %w", err)
		}
	}

	logger.Info("Validando o TAR.GZ local: %s", archivePath)
	if !dryRun {
		if err := validateTarGz(archivePath); err != nil {
			return fmt.Errorf("arquivo compactado inválido, não será enviado: %w", err)
		}
	}

	destB2Path := fmt.Sprintf("%s/%s/%s", cfg.B2Prefix, spec.b2Subfolder, archiveName)
	logger.Info("Enviando exclusivamente o TAR.GZ final para: b2://%s/%s", cfg.B2Bucket, destB2Path)

	if dryRun {
		logger.Info("DRY-RUN: upload para b2://%s/%s", cfg.B2Bucket, destB2Path)
	} else {
		if err := uploadToB2(cfg, archivePath, destB2Path, logger); err != nil {
			return fmt.Errorf("falha no upload para o B2: %w", err)
		}
		logger.Info("Upload concluído: %s", archiveName)
	}
	return nil
}

func runPgDump(cfg Config, extraArgs []string, outputFile string) error {
	args := []string{
		"-h", cfg.PGHost,
		"-p", cfg.PGPort,
		"-U", cfg.PGUser,
		"-d", cfg.PGDatabase,
	}
	args = append(args, extraArgs...)
	args = append(args, "--file", outputFile)

	cmd := exec.Command("pg_dump", args...)
	cmd.Env = pgEnv(cfg)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, string(out))
	}
	return nil
}

func pgEnv(cfg Config) []string {
	env := os.Environ()
	if cfg.PGPassword != "" {
		env = append(env, "PGPASSWORD="+cfg.PGPassword)
	}
	return env
}

func uploadToB2(cfg Config, localPath, destPath string, logger *Logger) error {
	client := b2.New(cfg.B2KeyID, cfg.B2AppKey)
	if err := client.Authorize(); err != nil {
		return err
	}

	bucket, err := client.FindBucketByName(cfg.B2Bucket)
	if err != nil {
		return fmt.Errorf("falha ao localizar o bucket %s: %w", cfg.B2Bucket, err)
	}
	if bucket == nil {
		return fmt.Errorf("bucket %s não encontrado ou chave sem acesso a ele", cfg.B2Bucket)
	}

	info, err := os.Stat(localPath)
	if err != nil {
		return err
	}

	if info.Size() >= b2.LargeFileThreshold {
		f, err := os.Open(localPath)
		if err != nil {
			return err
		}
		defer f.Close()
		lastLogged := int64(0)
		return client.UploadLargeFile(bucket.BucketID, destPath, f, info.Size(), func(uploaded, total int64) {
			// Log progress roughly every 100MB to keep the log useful on
			// slow links without flooding it.
			if uploaded-lastLogged >= 100*1024*1024 || uploaded == total {
				logger.Info("Progresso do upload: %d/%d MB", uploaded/1024/1024, total/1024/1024)
				lastLogged = uploaded
			}
		})
	}

	data, err := os.ReadFile(localPath)
	if err != nil {
		return err
	}
	return client.UploadSmallFile(bucket.BucketID, destPath, data)
}

// cleanupLocalRetention removes local .backup/.tar.gz files older than
// the configured retention — the long-term copy lives in B2 (with its
// own lifecycle rules); local copies are just a short-term safety net.
func cleanupLocalRetention(cfg Config) {
	entries, err := os.ReadDir(cfg.WorkDir)
	if err != nil {
		return
	}
	now := time.Now()
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		age := now.Sub(info.ModTime())
		name := e.Name()
		switch {
		case hasSuffix(name, ".backup") && age > time.Duration(cfg.RetentionDumpDays)*24*time.Hour:
			_ = os.Remove(filepath.Join(cfg.WorkDir, name))
		case hasSuffix(name, ".tar.gz") && age > time.Duration(cfg.RetentionZipDays)*24*time.Hour:
			_ = os.Remove(filepath.Join(cfg.WorkDir, name))
		}
	}
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "desconhecido"
	}
	return h
}
