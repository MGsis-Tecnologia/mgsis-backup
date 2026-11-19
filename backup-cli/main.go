// Command mgsis-backup is the single dependency-free replacement for
// setup_b2_bucket.sh / backup_uni.sh / backup_verifica.sh, for new
// client installations. It talks to Backblaze B2 directly (no rclone,
// no b2-CLI, no Python) and relies on only one external program that
// must already be present on the machine: the PostgreSQL client tools
// (pg_dump, psql, createdb, dropdb) — exactly as decided, since
// reimplementing the dump/restore logic in Go was explicitly ruled out.
//
// Usage:
//
//	mgsis-backup setup --client-id 001234 --pg-database meubanco \
//	    --master-key-id <...> --master-app-key <...>
//	mgsis-backup diario [--dry-run] [--config path]
//	mgsis-backup fotos  [--dry-run] [--config path]
//	mgsis-backup logs   [--dry-run] [--config path]
//	mgsis-backup verifica [--dry-run] [--config path]
//	mgsis-backup teste-b2 [--config path]
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

	// Every other command shares the same two flags, so parse them once.
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	configPath := fs.String("config", "", "Caminho do config.json (padrão: ao lado do executável)")
	dryRun := fs.Bool("dry-run", false, "Simula a execução sem gerar dump, compactar ou enviar nada")
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
		fmt.Fprintln(os.Stderr, "Rode 'mgsis-backup setup ...' primeiro, ou aponte --config para o arquivo correto.")
		return 1
	}

	switch cmd {
	case "diario", "fotos", "logs":
		return runBackup(cfg, cmd, *dryRun)
	case "verifica":
		return runVerifica(cfg, *dryRun)
	case "teste-b2":
		return runTesteB2(cfg)
	default:
		fmt.Fprintf(os.Stderr, "Comando desconhecido: %s\n\n", cmd)
		printUsage()
		return 2
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `mgsis-backup — backup PostgreSQL -> Backblaze B2

Comandos:
  setup       Provisiona um novo cliente (bucket, chave B2, config.json, agendamento)
  diario      Executa o backup diário (exclui fotos e log_sistema)
  fotos       Executa o backup da tabela de fotos
  logs        Executa o backup da tabela de log_sistema
  verifica    Roda a verificação de integridade (hash produção x restaurado)
  teste-b2    Testa autenticação e acesso ao bucket configurado

Flags comuns a diario/fotos/logs/verifica/teste-b2:
  --config <path>   Caminho do config.json (padrão: ao lado do executável)
  --dry-run         Simula sem executar ações reais (não vale para teste-b2)

Para opções do setup: mgsis-backup setup -h`)
}
