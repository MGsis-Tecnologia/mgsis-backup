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

func runBackup(cfg Config, profileName string, dryRun bool) int {
	spec, err := profileFor(profileName, cfg.ClientID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Erro:", err)
		return 1
	}

	logger, err := NewLogger(cfg.WorkDir, spec.name, fmt.Sprintf("backup-%s.log", spec.name))
	if err != nil {
		fmt.Fprintln(os.Stderr, "Erro ao abrir log:", err)
		return 1
	}
	defer logger.Close()

	release, err := acquireLock(cfg.WorkDir, "backup-"+spec.name, 6*time.Hour)
	if err != nil {
		logger.Warn("%s", err)
		return 75
	}
	defer release()

	logger.Info("Iniciando backup no host %s; cliente %s.", hostname(), cfg.ClientID)

	if err := os.MkdirAll(cfg.WorkDir, 0700); err != nil {
		logger.Fatal(1, "Falha ao criar diretório de trabalho: %v", err)
	}

	cleanupLocalRetention(cfg)

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
			logger.Fatal(1, "Falha no pg_dump: %v", err)
		}
		if err := os.Rename(dumpPath+".partial", dumpPath); err != nil {
			logger.Fatal(1, "Falha ao renomear dump: %v", err)
		}
	}

	logger.Info("Compactando em: %s", archiveName)
	if dryRun {
		logger.Info("DRY-RUN: compactar %s -> %s", dumpPath, archivePath)
	} else {
		if err := compressToTarGz(dumpPath, archivePath+".partial"); err != nil {
			logger.Fatal(1, "Falha ao compactar: %v", err)
		}
		if err := os.Rename(archivePath+".partial", archivePath); err != nil {
			logger.Fatal(1, "Falha ao renomear arquivo compactado: %v", err)
		}
	}

	logger.Info("Validando o TAR.GZ local: %s", archivePath)
	if !dryRun {
		if err := validateTarGz(archivePath); err != nil {
			logger.Fatal(1, "Arquivo compactado inválido, não será enviado: %v", err)
		}
	}

	destB2Path := fmt.Sprintf("%s/%s/%s", cfg.B2Prefix, spec.b2Subfolder, archiveName)
	logger.Info("Enviando exclusivamente o TAR.GZ final para: b2://%s/%s", cfg.B2Bucket, destB2Path)

	if dryRun {
		logger.Info("DRY-RUN: upload para b2://%s/%s", cfg.B2Bucket, destB2Path)
	} else {
		if err := uploadToB2(cfg, archivePath, destB2Path, logger); err != nil {
			logger.Fatal(1, "Falha no upload para o B2: %v", err)
		}
		logger.Info("Upload concluído: %s", archiveName)
	}

	logger.Info("SUCESSO: rotina concluída.")
	return 0
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
