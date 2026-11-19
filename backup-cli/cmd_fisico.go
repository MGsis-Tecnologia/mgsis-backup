package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Exit codes kept from backup_uni.sh's --fisico profile.
const (
	exitUsage       = 64 // invalid PHYSICAL_METHOD
	exitUnavailable = 69 // pg_basebackup not installed
)

// physicalRootDir is the single top-level folder inside the archive.
// Restores depend on this exact name — do not change it.
const physicalRootDir = "main"

// runFisico is the port of backup_uni.sh --fisico: a .tar.gz of the
// PostgreSQL data directory (not a pg_dump), uploaded to
// {b2_prefix}/fisico/. The data directory holds every database of the
// server, so this runs once per server and goes to a single bucket: the
// one of physical_client_id, or of the first database in "bancos" (main
// narrows cfg.Databases to that one before calling here).
func runFisico(cfg Config, dryRun bool) int {
	logger, err := NewLogger(cfg.WorkDir, "fisico", "backup-fisico.log")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Erro ao abrir log:", err)
		return 1
	}
	defer logger.Close()

	// Physical backups of big servers can take hours; give the lock more
	// room than the dump profiles before treating it as stale.
	release, err := acquireLock(cfg.WorkDir, "backup-fisico", 12*time.Hour)
	if err != nil {
		logger.Warn("%s", err)
		return 75
	}
	defer release()

	if err := cfg.ValidatePhysical(); err != nil {
		logger.Error("%v", err)
		if errors.Is(err, errInvalidPhysicalMethod) {
			return exitUsage
		}
		return 1
	}
	method := cfg.physicalMethod()

	basebackupBin := ""
	if method == physicalPgBasebackup {
		basebackupBin, err = exec.LookPath("pg_basebackup")
		if err != nil {
			logger.Error("pg_basebackup não encontrado no PATH: %v", err)
			return exitUnavailable
		}
	}

	t := cfg.Targets()[0]
	logger.SetScope("fisico:" + t.ClientID)
	logger.Info("Iniciando backup físico no host %s; método %s; destino bucket %s (cliente %s).", hostname(), method, t.B2Bucket, t.ClientID)
	if len(cfg.Databases) > 1 {
		// Only reachable with a hand-edited call path; main always narrows
		// to one. Kept explicit so the behaviour is never ambiguous.
		logger.Warn("Mais de um banco recebido; o backup físico vai só para o bucket de %s.", t.ClientID)
	}

	fisicoDir := filepath.Join(cfg.WorkDir, "fisico")
	if err := os.MkdirAll(fisicoDir, 0700); err != nil {
		logger.Error("Falha ao criar diretório %s: %v", fisicoDir, err)
		return 1
	}
	cleanupFisicoRetention(fisicoDir, logger)

	prefix := t.ClientID + "-fisico"
	timestamp := time.Now().Format("02-01-06-15-04")
	archiveName := fmt.Sprintf("%s-%s.tar.gz", prefix, timestamp)
	archivePath := filepath.Join(fisicoDir, archiveName)
	partialPath := archivePath + ".partial"

	checkFreeSpace(t, method, fisicoDir, logger)

	if dryRun {
		switch method {
		case physicalLegacyCopy:
			logger.Warn("Usando cópia legada do diretório PostgreSQL em uso. Para recuperação consistente, prefira PHYSICAL_METHOD=pg_basebackup.")
			logger.Info("DRY-RUN: empacotar %s como %s/ em %s", t.PhysicalSourceDir, physicalRootDir, partialPath)
		case physicalPgBasebackup:
			work := filepath.Join(fisicoDir, fmt.Sprintf(".work-%s-%s-%d", prefix, timestamp, os.Getpid()))
			logger.Info("DRY-RUN: %s -h %s -p %s -U %s -D %s -Fp -Xs -P", basebackupBin, t.PGHost, t.PGPort, t.PGUser, filepath.Join(work, physicalRootDir))
			logger.Info("DRY-RUN: empacotar %s em %s", filepath.Join(work, physicalRootDir), partialPath)
		}
		logger.Info("DRY-RUN: validar, renomear para %s e enviar para b2://%s/%s/fisico/%s", archiveName, t.B2Bucket, t.B2Prefix, archiveName)
		logger.Info("SUCESSO: rotina concluída (dry-run).")
		return 0
	}

	if err := buildPhysicalArchive(t, method, basebackupBin, fisicoDir, prefix, timestamp, partialPath, logger); err != nil {
		_ = os.Remove(partialPath)
		logger.Error("%v", err)
		return 1
	}

	logger.Info("Validando o TAR.GZ local: %s", partialPath)
	if err := validateTarGz(partialPath); err != nil {
		_ = os.Remove(partialPath)
		logger.Error("Arquivo compactado inválido, não será enviado: %v", err)
		return 1
	}
	if err := os.Rename(partialPath, archivePath); err != nil {
		logger.Error("Falha ao renomear arquivo compactado: %v", err)
		return 1
	}
	if info, err := os.Stat(archivePath); err == nil {
		logger.Info("Arquivo pronto: %s (%s).", archiveName, humanBytes(uint64(info.Size())))
	}

	destB2Path := fmt.Sprintf("%s/fisico/%s", t.B2Prefix, archiveName)
	logger.Info("Enviando para: b2://%s/%s", t.B2Bucket, destB2Path)
	if err := uploadToB2(t, archivePath, destB2Path, logger); err != nil {
		logger.Error("Falha no upload para o B2: %v", err)
		return 1
	}
	logger.Info("Upload concluído: %s", archiveName)
	logger.Info("SUCESSO: rotina concluída.")
	return 0
}

// buildPhysicalArchive writes partialPath for the chosen method.
func buildPhysicalArchive(cfg Config, method, basebackupBin, fisicoDir, prefix, timestamp, partialPath string, logger *Logger) error {
	switch method {
	case physicalLegacyCopy:
		logger.Warn("Usando cópia legada do diretório PostgreSQL em uso. Para recuperação consistente, prefira PHYSICAL_METHOD=pg_basebackup.")
		logger.Info("Empacotando %s (sem cópia intermediária).", cfg.PhysicalSourceDir)
		return writeDirToTarGz(cfg.PhysicalSourceDir, physicalRootDir, partialPath, logger)

	case physicalPgBasebackup:
		work := filepath.Join(fisicoDir, fmt.Sprintf(".work-%s-%s-%d", prefix, timestamp, os.Getpid()))
		if err := os.MkdirAll(work, 0700); err != nil {
			return fmt.Errorf("falha ao criar diretório temporário %s: %w", work, err)
		}
		defer os.RemoveAll(work)

		dest := filepath.Join(work, physicalRootDir)
		logger.Info("Rodando pg_basebackup para %s.", dest)
		cmd := exec.Command(basebackupBin,
			"-h", cfg.PGHost, "-p", cfg.PGPort, "-U", cfg.PGUser,
			"-D", dest, "-Fp", "-Xs", "-P")
		cmd.Env = pgEnv(cfg)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("falha no pg_basebackup (o usuário precisa de permissão REPLICATION e de uma entrada de replicação no pg_hba.conf): %v: %s", err, tail(string(out), 2000))
		}
		logger.Info("pg_basebackup concluído; empacotando.")
		return writeDirToTarGz(dest, physicalRootDir, partialPath, logger)

	default:
		return fmt.Errorf("método físico desconhecido: %s", method)
	}
}

// writeDirToTarGz streams srcDir straight into a .tar.gz with every entry
// under rootName/ — no intermediate copy, so the disk only needs room
// for the compressed result. It keeps mode, uid/gid and symlinks as
// symlinks (pg_wal and pg_tblspc often are), like `cp -a` + `tar -czf`.
//
// The source may be a live data directory: a file that disappears
// between listing and reading is logged as WARN and skipped, and a file
// that shrinks while being read is zero-padded to the size announced in
// its header (what GNU tar does) — neither aborts the backup. Any other
// read error does. Sockets, devices and pipes are skipped.
func writeDirToTarGz(srcDir, rootName, destPath string, logger *Logger) error {
	srcDir = filepath.Clean(srcDir)

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	bw := bufio.NewWriterSize(out, 1<<20)
	gz := gzip.NewWriter(bw)
	tw := tar.NewWriter(gz)

	var files, skipped int
	walkErr := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && path != srcDir {
				logger.Warn("Sumiu durante a leitura, ignorado: %s", path)
				skipped++
				return nil
			}
			return fmt.Errorf("falha ao ler %s: %w", path, err)
		}

		info, err := os.Lstat(path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				logger.Warn("Sumiu durante a leitura, ignorado: %s", path)
				skipped++
				return nil
			}
			return fmt.Errorf("falha ao ler %s: %w", path, err)
		}

		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		name := rootName
		if rel != "." {
			name = rootName + "/" + filepath.ToSlash(rel)
		}

		mode := info.Mode()
		if mode&(fs.ModeSocket|fs.ModeNamedPipe|fs.ModeDevice|fs.ModeCharDevice|fs.ModeIrregular) != 0 {
			logger.Warn("Arquivo especial ignorado: %s", path)
			skipped++
			return nil
		}

		link := ""
		if mode&fs.ModeSymlink != 0 {
			link, err = os.Readlink(path)
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					logger.Warn("Sumiu durante a leitura, ignorado: %s", path)
					skipped++
					return nil
				}
				return fmt.Errorf("falha ao ler o link %s: %w", path, err)
			}
		}

		hdr, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return fmt.Errorf("falha ao montar cabeçalho de %s: %w", path, err)
		}
		hdr.Name = name
		if info.IsDir() {
			hdr.Name += "/"
		}

		if !mode.IsRegular() {
			// Directory or symlink: header only. WalkDir never follows
			// symlinks, so a symlinked pg_wal is stored as the link itself.
			return tw.WriteHeader(hdr)
		}

		f, err := os.Open(path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				logger.Warn("Sumiu durante a leitura, ignorado: %s", path)
				skipped++
				return nil
			}
			return fmt.Errorf("falha ao abrir %s: %w", path, err)
		}
		defer f.Close()

		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("falha ao escrever cabeçalho de %s: %w", path, err)
		}
		// Copy exactly the size in the header: a file that grew is cut at
		// that size, one that shrank is padded with zeros.
		n, err := io.CopyN(tw, f, hdr.Size)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return fmt.Errorf("falha ao ler %s: %w", path, err)
			}
			logger.Warn("Arquivo diminuiu durante a leitura (%d de %d bytes), completado com zeros: %s", n, hdr.Size, path)
			if _, err := io.CopyN(tw, zeroReader{}, hdr.Size-n); err != nil {
				return fmt.Errorf("falha ao completar %s: %w", path, err)
			}
		}
		files++
		return nil
	})
	if walkErr != nil {
		return walkErr
	}

	// Close in dependency order and check every error: a silently
	// truncated archive is exactly what must never reach B2.
	if err := tw.Close(); err != nil {
		return fmt.Errorf("falha ao finalizar tar: %w", err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("falha ao finalizar gzip: %w", err)
	}
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("falha ao gravar %s: %w", destPath, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("falha ao gravar %s: %w", destPath, err)
	}
	logger.Info("Empacotados %d arquivo(s); %d ignorado(s).", files, skipped)
	return nil
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

// cleanupFisicoRetention keeps 30 days of local physical archives (as the
// script did) and removes leftovers of crashed runs: .partial files and
// .work-* directories older than a day.
func cleanupFisicoRetention(dir string, logger *Logger) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	now := time.Now()
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		age := now.Sub(info.ModTime())
		name := e.Name()
		path := filepath.Join(dir, name)
		switch {
		case e.IsDir() && strings.HasPrefix(name, ".work-") && age > 24*time.Hour:
			logger.Info("Removendo temporário antigo: %s", name)
			_ = os.RemoveAll(path)
		case !e.IsDir() && hasSuffix(name, ".partial") && age > 24*time.Hour:
			logger.Info("Removendo arquivo incompleto antigo: %s", name)
			_ = os.Remove(path)
		case !e.IsDir() && hasSuffix(name, ".tar.gz") && age > 30*24*time.Hour:
			logger.Info("Retenção local: removendo %s", name)
			_ = os.Remove(path)
		}
	}
}

// checkFreeSpace only warns: the estimate is rough (compression ratio
// varies a lot) and a false alarm must never stop a backup.
func checkFreeSpace(cfg Config, method, dir string, logger *Logger) {
	free, err := freeDiskBytes(dir)
	if err != nil {
		logger.Warn("Não consegui verificar o espaço livre em %s: %v", dir, err)
		return
	}

	var size uint64
	switch method {
	case physicalLegacyCopy:
		size, err = dirSize(cfg.PhysicalSourceDir)
	case physicalPgBasebackup:
		size, err = clusterSize(cfg)
	}
	if err != nil {
		logger.Warn("Não consegui estimar o tamanho dos dados: %v (espaço livre: %s)", err, humanBytes(free))
		return
	}

	// The compressed archive is at most about the size of the data;
	// pg_basebackup also needs a full uncompressed copy first.
	need := size
	if method == physicalPgBasebackup {
		need = 2 * size
	}
	logger.Info("Dados: %s; espaço livre em %s: %s.", humanBytes(size), dir, humanBytes(free))
	if free < need {
		logger.Warn("Espaço livre pode ser insuficiente: até %s necessários, %s disponíveis.", humanBytes(need), humanBytes(free))
	}
}

func dirSize(root string) (uint64, error) {
	var total uint64
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				total += uint64(info.Size())
			}
		}
		return nil
	})
	return total, err
}

func clusterSize(cfg Config) (uint64, error) {
	out, err := runPsqlQuery(cfg, cfg.PGDatabase, "SELECT sum(pg_database_size(oid))::bigint FROM pg_database;")
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(out), 10, 64)
}

func humanBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}
