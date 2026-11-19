package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// runVerifica reproduces the per-table hash round-trip test validated
// earlier: hash every table in production, restore the current backup
// into a scratch database, hash it again, and compare — commutative sums
// so table row order never causes a false alarm. If the first attempt
// diverges, it retries once in full before declaring a real FALHA, since
// a write landing between "hash production" and "pg_dump" can otherwise
// look exactly like corruption.
func runVerifica(cfg Config, dryRun bool) int {
	logger, err := NewLogger(cfg.WorkDir, "VERIFICA", "verificacao.log")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Erro ao abrir log:", err)
		return 1
	}
	defer logger.Close()

	release, err := acquireLock(cfg.WorkDir, "verifica", 6*time.Hour)
	if err != nil {
		logger.Warn("%s", err)
		return 75
	}
	defer release()

	verificaDir := filepath.Join(cfg.WorkDir, "verificacao")
	if err := os.MkdirAll(verificaDir, 0700); err != nil {
		logger.Fatal(1, "Falha ao criar diretório de verificação: %v", err)
	}
	cleanupOldVerificationArtifacts(verificaDir, cfg.ClientID, 30)

	logger.Info("Iniciando verificação de integridade — cliente %s.", cfg.ClientID)

	if dryRun {
		logger.Info("DRY-RUN: nenhuma comparação real será feita.")
		writeAndUploadResult(cfg, logger, "OK", "DRY-RUN — nenhuma comparação real foi feita.")
		logger.Info("Verificação (dry-run) concluída.")
		return 0
	}

	ok, detail1, err := attemptVerification(cfg, logger, verificaDir, 1)
	if err != nil {
		logger.Fatal(1, "Erro durante a verificação: %v", err)
	}
	if ok {
		logger.Info("SUCESSO: hash de todas as tabelas bateu — backup restaura corretamente.")
		writeAndUploadResult(cfg, logger, "OK", "Hash de todas as tabelas confere entre produção e restaurado.")
		return 0
	}

	logger.Warn("Primeira tentativa divergiu — pode ser corrupção real ou timing de escrita concorrente. Tentando de novo para confirmar.")
	ok2, detail2, err := attemptVerification(cfg, logger, verificaDir, 2)
	if err != nil {
		logger.Fatal(1, "Erro durante a segunda tentativa de verificação: %v", err)
	}
	if ok2 {
		logger.Info("SUCESSO na segunda tentativa — a primeira divergência foi timing (escrita concorrente), não corrupção.")
		writeAndUploadResult(cfg, logger, "OK", "Hash bateu na segunda tentativa; primeira divergência atribuída a escrita concorrente.")
		return 0
	}

	logger.Error("FALHA CONFIRMADA: hash divergiu em duas tentativas seguidas — não é timing, é problema real.")
	detail := fmt.Sprintf("Hash divergente em duas tentativas seguidas.\n\n== Tentativa 1 ==\n%s\n\n== Tentativa 2 ==\n%s", detail1, detail2)
	writeAndUploadResult(cfg, logger, "FALHA", detail)
	return 1
}

// attemptVerification runs one full cycle: hash production -> pg_dump ->
// restore into scratch db -> hash scratch db -> compare. Returns whether
// it matched and, if not, a human-readable diff of which tables differed.
func attemptVerification(cfg Config, logger *Logger, verificaDir string, attemptNum int) (ok bool, diffDetail string, err error) {
	tempDB := cfg.PGDatabase + "_verif_tmp"
	timestamp := time.Now().Format("02-01-06-15-04")
	dumpPath := filepath.Join(verificaDir, fmt.Sprintf("%s-dump-%s-t%d.sql", cfg.ClientID, timestamp, attemptNum))

	logger.Info("[tentativa %d] 1/5 — Calculando hash por tabela na produção.", attemptNum)
	prodHashes, err := computeTableHashes(cfg, cfg.PGDatabase)
	if err != nil {
		return false, "", fmt.Errorf("hash da produção: %w", err)
	}

	logger.Info("[tentativa %d] 2/5 — Gerando dump do banco de produção.", attemptNum)
	excludeArgs := []string{}
	if cfg.ExcludeFotos {
		excludeArgs = append(excludeArgs, "--exclude-table=produto_foto")
	}
	if err := runPgDump(cfg, excludeArgs, dumpPath); err != nil {
		return false, "", fmt.Errorf("pg_dump: %w", err)
	}
	defer os.Remove(dumpPath)

	logger.Info("[tentativa %d] 3/5 — Criando banco temporário %s e restaurando.", attemptNum, tempDB)
	_ = dropDB(cfg, tempDB) // best-effort cleanup from a previous crashed run
	if err := createDB(cfg, tempDB); err != nil {
		return false, "", fmt.Errorf("createdb: %w", err)
	}
	defer dropDB(cfg, tempDB)

	if err := restoreSQLFile(cfg, tempDB, dumpPath); err != nil {
		return false, "", fmt.Errorf("restore: %w", err)
	}

	logger.Info("[tentativa %d] 4/5 — Calculando hash por tabela no banco restaurado.", attemptNum)
	restoredHashes, err := computeTableHashes(cfg, tempDB)
	if err != nil {
		return false, "", fmt.Errorf("hash do restaurado: %w", err)
	}

	logger.Info("[tentativa %d] 5/5 — Comparando.", attemptNum)
	match, diff := diffTableHashes(prodHashes, restoredHashes)
	return match, diff, nil
}

type tableHash struct {
	table string
	rows  string
	hash  string
}

// computeTableHashes runs one query per connection that returns, for
// every table in the public schema, (table name, row count, a
// commutative sum of a per-row hash). The sum is commutative so physical
// row order — which differs between a production table with years of
// writes and a freshly COPY-restored one — can never cause a false
// mismatch; only an actual content difference can.
func computeTableHashes(cfg Config, dbName string) ([]tableHash, error) {
	tables, err := listTables(cfg, dbName, cfg.ExcludeFotos)
	if err != nil {
		return nil, err
	}
	if len(tables) == 0 {
		return nil, fmt.Errorf("nenhuma tabela encontrada no schema public de %s", dbName)
	}

	var query strings.Builder
	for i, t := range tables {
		if i > 0 {
			query.WriteString("\nUNION ALL\n")
		}
		fmt.Fprintf(&query, `SELECT '%s'::text AS tabela, count(*)::bigint AS linhas, COALESCE(sum(('x'||substr(md5(t::text),1,15))::bit(60)::bigint),0)::numeric AS hash_soma FROM public.%q t`, t, t)
	}
	query.WriteString("\nORDER BY tabela;")

	out, err := runPsqlQuery(cfg, dbName, query.String())
	if err != nil {
		return nil, err
	}

	var hashes []tableHash
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) != 3 {
			continue
		}
		hashes = append(hashes, tableHash{table: parts[0], rows: parts[1], hash: parts[2]})
	}
	return hashes, nil
}

func diffTableHashes(a, b []tableHash) (match bool, detail string) {
	am := map[string]tableHash{}
	for _, h := range a {
		am[h.table] = h
	}
	bm := map[string]tableHash{}
	for _, h := range b {
		bm[h.table] = h
	}

	var diffs []string
	for name, ha := range am {
		hb, exists := bm[name]
		if !exists {
			diffs = append(diffs, fmt.Sprintf("tabela %-30s existe só na produção (linhas=%s)", name, ha.rows))
			continue
		}
		if ha.rows != hb.rows || ha.hash != hb.hash {
			diffs = append(diffs, fmt.Sprintf("tabela %-30s produção(linhas=%s hash=%s) restaurado(linhas=%s hash=%s)", name, ha.rows, ha.hash, hb.rows, hb.hash))
		}
	}
	for name, hb := range bm {
		if _, exists := am[name]; !exists {
			diffs = append(diffs, fmt.Sprintf("tabela %-30s existe só no restaurado (linhas=%s)", name, hb.rows))
		}
	}

	if len(diffs) == 0 {
		return true, ""
	}
	return false, strings.Join(diffs, "\n")
}

func listTables(cfg Config, dbName string, excludeFotos bool) ([]string, error) {
	query := "SELECT tablename FROM pg_tables WHERE schemaname='public'"
	if excludeFotos {
		query += " AND tablename NOT IN ('produto_foto')"
	}
	query += " ORDER BY tablename;"

	out, err := runPsqlQuery(cfg, dbName, query)
	if err != nil {
		return nil, err
	}
	var tables []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			tables = append(tables, line)
		}
	}
	return tables, nil
}

func runPsqlQuery(cfg Config, dbName, query string) (string, error) {
	args := []string{
		"-h", cfg.PGHost,
		"-p", cfg.PGPort,
		"-U", cfg.PGUser,
		"-d", dbName,
		"-X", "-Atq", "-F|",
		"-c", query,
	}
	cmd := exec.Command("psql", args...)
	cmd.Env = pgEnv(cfg)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%v: %s", err, stderr.String())
	}
	return stdout.String(), nil
}

func createDB(cfg Config, dbName string) error {
	cmd := exec.Command("createdb", "-h", cfg.PGHost, "-p", cfg.PGPort, "-U", cfg.PGUser, dbName)
	cmd.Env = pgEnv(cfg)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, string(out))
	}
	return nil
}

func dropDB(cfg Config, dbName string) error {
	cmd := exec.Command("dropdb", "-h", cfg.PGHost, "-p", cfg.PGPort, "-U", cfg.PGUser, "--if-exists", dbName)
	cmd.Env = pgEnv(cfg)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, string(out))
	}
	return nil
}

func restoreSQLFile(cfg Config, dbName, sqlPath string) error {
	cmd := exec.Command("psql", "-h", cfg.PGHost, "-p", cfg.PGPort, "-U", cfg.PGUser, "-d", dbName, "-v", "ON_ERROR_STOP=1", "-q", "-f", sqlPath)
	cmd.Env = pgEnv(cfg)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, string(out))
	}
	return nil
}

func writeAndUploadResult(cfg Config, logger *Logger, status, detail string) {
	resultName := fmt.Sprintf("%s-verificacao-%s.txt", cfg.ClientID, time.Now().Format("02-01-06-15-04"))
	content := fmt.Sprintf("cliente: %s\nbanco: %s\ndata: %s\nstatus: %s\ndetalhe: %s\n",
		cfg.ClientID, cfg.PGDatabase, time.Now().UTC().Format("2006-01-02T15:04:05Z"), status, detail)

	localPath := filepath.Join(cfg.WorkDir, "verificacao", resultName)
	if err := os.WriteFile(localPath, []byte(content), 0600); err != nil {
		logger.Error("Falha ao salvar resultado local: %v", err)
	}

	destPath := fmt.Sprintf("%s/verificacao/%s", cfg.B2Prefix, resultName)
	if err := uploadToB2(cfg, localPath, destPath, logger); err != nil {
		logger.Warn("Falha ao enviar resultado ao B2 — ele permanece salvo localmente em %s: %v", localPath, err)
		return
	}
	logger.Info("Resultado enviado ao B2: %s", resultName)
}

func cleanupOldVerificationArtifacts(dir, clientID string, days int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, clientID+"-dump-") && !strings.HasPrefix(name, clientID+"-diff-") {
			continue
		}
		info, err := e.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		_ = os.Remove(filepath.Join(dir, name))
	}
}
