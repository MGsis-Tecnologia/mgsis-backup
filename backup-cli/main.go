// Command mgsis-backup is the single dependency-free replacement for
// setup_b2_bucket.sh / backup_uni.sh / backup_verifica.sh, for new
// client installations. It talks to Backblaze B2 directly (no rclone,
// no b2-CLI, no Python) and relies on only one external program that
// must already be present on the machine: the PostgreSQL client tools
// (pg_dump, psql, createdb, dropdb) — exactly as decided, since
// reimplementing the dump/restore logic in Go was explicitly ruled out.
//
// One server can host several databases (one client / one bucket each).
// config.json lists them under "bancos", and every command below runs
// for each database in turn — one failing never stops the others.
//
// Usage:
//
//	mgsis-backup setup                       (assistente interativo)
//	mgsis-backup setup --banco 001234:meubanco [--banco 005678:outrobanco] \
//	    --master-key-id <...> --master-app-key <...>
//	mgsis-backup diario [--dry-run] [--config path] [--banco client_id]
//	mgsis-backup fotos  [--dry-run] [--config path] [--banco client_id]
//	mgsis-backup logs   [--dry-run] [--config path] [--banco client_id]
//	mgsis-backup fisico [--dry-run] [--config path] [--banco client_id]
//	mgsis-backup verifica [--dry-run] [--config path] [--banco client_id]
//	mgsis-backup teste-b2 [--config path] [--banco client_id]
//
// config.json lives beside the executable unless --config overrides it;
// `setup` creates it, so day-to-day runs don't need any flags at all —
// which is what the scheduled cron/Task Scheduler entries call.
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		printUsage()
		return 2
	}

	cmd := args[0]
	rest := args[1:]

	if cmd == "setup" {
		return runSetup(rest)
	}

	if cmd == "-h" || cmd == "--help" || cmd == "help" {
		printUsage()
		return 0
	}

	// Every other command shares the same flags, so parse them once.
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	configPath := fs.String("config", "", "Caminho do config.json (padrão: ao lado do executável)")
	dryRun := fs.Bool("dry-run", false, "Simula a execução sem gerar dump, compactar ou enviar nada")
	only := fs.String("banco", "", "Roda só para o banco com este client_id (padrão: todos do config.json)")
	if err := fs.Parse(rest); err != nil {
		return 2
	}

	cp := *configPath
	if cp == "" {
		cp = defaultConfigPath()
	}
	cfg, err := LoadConfig(cp)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Erro ao carregar configuração:", err)
		fmt.Fprintln(os.Stderr, "Rode 'mgsis-backup setup' primeiro, ou aponte --config para o arquivo correto.")
		return 1
	}

	// The physical backup covers the whole server (every database lives in
	// the same data directory), so it goes to one bucket only: the one
	// named by --banco, else physical_client_id, else the first database.
	if cmd == "fisico" && *only == "" {
		if cfg.PhysicalClientID != "" {
			*only = cfg.PhysicalClientID
		} else {
			*only = cfg.Databases[0].ClientID
		}
	}

	if *only != "" {
		var picked []DatabaseEntry
		for _, d := range cfg.Databases {
			if d.ClientID == *only {
				picked = append(picked, d)
			}
		}
		if len(picked) == 0 {
			fmt.Fprintf(os.Stderr, "Nenhum banco com client_id %q em %s. Bancos configurados:\n", *only, cp)
			for _, d := range cfg.Databases {
				fmt.Fprintf(os.Stderr, "  %s (banco %s, bucket %s)\n", d.ClientID, d.PGDatabase, d.B2Bucket)
			}
			return 2
		}
		cfg.Databases = picked
	}

	switch cmd {
	case "diario", "fotos", "logs":
		return runBackup(cfg, cmd, *dryRun)
	case "fisico":
		return runFisico(cfg, *dryRun)
	case "verifica":
		return runVerifica(cfg, *dryRun)
	case "teste-b2":
		return runTesteB2All(cfg)
	default:
		fmt.Fprintf(os.Stderr, "Comando desconhecido: %s\n\n", cmd)
		printUsage()
		return 2
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `mgsis-backup — backup PostgreSQL -> Backblaze B2

Comandos:
  setup       Provisiona os bancos deste servidor (buckets, chaves B2, config.json, agendamento)
  diario      Executa o backup diário (exclui fotos e log_sistema)
  fotos       Executa o backup da tabela de fotos
  logs        Executa o backup da tabela de log_sistema
  fisico      Backup físico: .tar.gz do diretório de dados do PostgreSQL
              (physical_method legacy_copy ou pg_basebackup)
  verifica    Roda a verificação de integridade (hash produção x restaurado)
  teste-b2    Testa autenticação e acesso aos buckets configurados

Cada comando roda para todos os bancos do config.json, um após o outro.
O fisico é a exceção: cobre o servidor inteiro e roda uma vez só, enviando
para o bucket de physical_client_id (padrão: o primeiro banco).

Flags comuns a diario/fotos/logs/fisico/verifica/teste-b2:
  --config <path>   Caminho do config.json (padrão: ao lado do executável)
  --banco <id>      Roda só para o banco com este client_id (no fisico:
                    escolhe o bucket de destino)
  --dry-run         Simula sem executar ações reais (não vale para teste-b2)

Para opções do setup: mgsis-backup setup -h`)
}
