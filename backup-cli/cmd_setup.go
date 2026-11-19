package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mgsis-backup/b2"
)

// stdinReader is shared by every prompt so buffered input is never lost
// between questions (matters when answers are piped in).
var stdinReader = bufio.NewReader(os.Stdin)

// setupParams holds everything provisionClient needs, regardless of
// whether it came from command-line flags or from the interactive
// wizard below.
type setupParams struct {
	clientID      string
	pgDatabase    string
	pgUser        string
	pgHost        string
	pgPort        string
	pgPassword    string
	masterKeyID   string
	masterAppKey  string
	masterEnvPath string
	bucketName    string
	workDir       string
	configPath    string
	excludeFotos  bool
	noSchedule    bool

	// base is the starting Config (an existing config.json when found,
	// otherwise defaults), so fields the wizard doesn't ask about —
	// retention days, B2 prefix — are preserved instead of reset.
	base Config
	// usePresetB2 means config.json already carries working B2 key +
	// bucket: skip master-key provisioning and just verify access.
	usePresetB2 bool
}

// runSetup provisions a new client: it creates (or reuses) the B2 bucket
// and its lifecycle rules, mints a restricted application key scoped to
// that bucket, writes config.json beside the executable, and installs
// the scheduled runs (cron on Linux, Scheduled Tasks on Windows). This
// replaces setup_b2_bucket.sh — but, per the agreed scope, only for new
// installations; it is never run against a client already on the old
// .sh-based system.
//
// With no arguments at all ("mgsis-backup setup"), it falls back to the
// old interactive style — one question at a time, Enter to accept the
// default — instead of requiring every flag up front. Pass any flag
// (e.g. --client-id ...) to use the non-interactive, flag-driven mode
// instead, useful for scripting the install across many clients.
func runSetup(args []string) int {
	if len(args) == 0 {
		return provisionClient(readSetupParamsInteractive())
	}

	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	clientID := fs.String("client-id", "", "Identificador do cliente (obrigatório), ex: 001234")
	pgDatabase := fs.String("pg-database", "", "Nome do banco PostgreSQL (obrigatório)")
	pgUser := fs.String("pg-user", "postgres", "Usuário PostgreSQL")
	pgHost := fs.String("pg-host", "localhost", "Host PostgreSQL")
	pgPort := fs.String("pg-port", "5432", "Porta PostgreSQL")
	pgPassword := fs.String("pg-password", "", "Senha PostgreSQL (opcional, vazio se usar .pgpass/trust)")
	masterKeyID := fs.String("master-key-id", "", "keyID da chave master do B2 (só precisa na 1ª instalação deste servidor; depois fica salva localmente)")
	masterAppKey := fs.String("master-app-key", "", "applicationKey master do B2 (idem — só na 1ª vez)")
	masterEnvPath := fs.String("master-env", "", "Onde a chave master fica salva localmente (padrão: ~/.config/b2-auto/master.env, o mesmo caminho do setup_b2_bucket.sh antigo)")
	bucketName := fs.String("bucket-name", "", "Nome do bucket B2 (padrão: mgsis-backup-<client-id>)")
	workDir := fs.String("work-dir", "", "Diretório de trabalho para dumps/logs locais (padrão: /dados/backup no Linux, pasta do .exe no Windows)")
	configPath := fs.String("config", "", "Onde salvar o config.json (padrão: ao lado do executável)")
	excludeFotos := fs.Bool("exclude-fotos", true, "Excluir a tabela de fotos do perfil diário (fotos tem perfil próprio)")
	noSchedule := fs.Bool("no-schedule", false, "Não instalar o agendamento (cron/Tarefa Agendada) — só gerar o config.json")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *clientID == "" || *pgDatabase == "" {
		fmt.Fprintln(os.Stderr, "Erro: --client-id e --pg-database são obrigatórios.")
		fmt.Fprintln(os.Stderr, "(Ou rode 'mgsis-backup setup' sem nenhuma flag para responder as perguntas uma a uma.)")
		fs.PrintDefaults()
		return 2
	}

	envPath := *masterEnvPath
	if envPath == "" {
		envPath = defaultMasterEnvPath()
	}

	mKeyID, mAppKey := *masterKeyID, *masterAppKey
	if mKeyID == "" || mAppKey == "" {
		// Not given on the command line — fall back to the saved file,
		// exactly like setup_b2_bucket.sh did, so repeat installs on
		// this same server never need the master key again.
		savedKeyID, savedAppKey, ok := loadMasterEnv(envPath)
		if !ok {
			fmt.Fprintln(os.Stderr, "Erro: nenhuma chave master informada e nenhuma encontrada em", envPath)
			fmt.Fprintln(os.Stderr, "Informe --master-key-id e --master-app-key na primeira vez neste servidor (serão salvos para as próximas).")
			return 2
		}
		mKeyID, mAppKey = savedKeyID, savedAppKey
		fmt.Println("Usando credenciais master salvas em", envPath+".")
	} else if saveErr := saveMasterEnv(envPath, mKeyID, mAppKey); saveErr != nil {
		fmt.Fprintln(os.Stderr, "Aviso: não consegui salvar a chave master para reuso futuro:", saveErr)
	}

	return provisionClient(setupParams{
		clientID:      *clientID,
		pgDatabase:    *pgDatabase,
		pgUser:        *pgUser,
		pgHost:        *pgHost,
		pgPort:        *pgPort,
		pgPassword:    *pgPassword,
		masterKeyID:   mKeyID,
		masterAppKey:  mAppKey,
		masterEnvPath: envPath,
		bucketName:    *bucketName,
		workDir:       *workDir,
		configPath:    *configPath,
		excludeFotos:  *excludeFotos,
		noSchedule:    *noSchedule,
	})
}

// readSetupParamsInteractive asks one question at a time, exactly like
// the old setup_b2_bucket.sh prompts: shows the default in brackets,
// Enter alone accepts it. Only client-id, pg-database, master-key-id and
// master-app-key have no default and must be typed.
func readSetupParamsInteractive() setupParams {
	reader := stdinReader
	ask := func(label, def string) string {
		if def != "" {
			fmt.Printf("%s [%s]: ", label, def)
		} else {
			fmt.Printf("%s: ", label)
		}
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			return def
		}
		return line
	}
	askRequired := func(label string) string {
		for {
			v := ask(label, "")
			if v != "" {
				return v
			}
			fmt.Println("  (obrigatório, não pode ficar vazio)")
		}
	}

	// A config.json beside the executable (the same place the scheduled
	// runs read it from) pre-fills every answer below.
	cfgPath := defaultConfigPath()
	existing, found := LoadConfigLenient(cfgPath)

	fmt.Println("=== Instalação de cliente — mgsis-backup ===")
	if found {
		fmt.Println("Encontrei", cfgPath, "— os valores já preenchidos aparecem entre colchetes.")
	}
	fmt.Println("(Enter sem digitar nada mantém o valor entre colchetes.)")
	fmt.Println()

	p := setupParams{base: existing, configPath: cfgPath}
	askDefault := func(label, def string) string {
		if def == "" {
			return askRequired(label)
		}
		return ask(label, def)
	}
	p.clientID = askDefault("Identificador do cliente (ex: 001234)", existing.ClientID)
	p.pgDatabase = askDefault("Nome do banco PostgreSQL", existing.PGDatabase)
	p.pgUser = ask("Usuário PostgreSQL", existing.PGUser)
	p.pgHost = ask("Host PostgreSQL", existing.PGHost)
	p.pgPort = ask("Porta PostgreSQL", existing.PGPort)
	if existing.PGPassword != "" {
		pw := ask("Senha PostgreSQL (já preenchida; Enter mantém, '-' para limpar)", "")
		switch pw {
		case "":
			p.pgPassword = existing.PGPassword
		case "-":
			p.pgPassword = ""
		default:
			p.pgPassword = pw
		}
	} else {
		p.pgPassword = ask("Senha PostgreSQL (vazio se usar .pgpass/trust)", "")
	}
	fmt.Println()

	p.masterEnvPath = defaultMasterEnvPath()
	if existing.B2KeyID != "" && existing.B2AppKey != "" && existing.B2Bucket != "" {
		fmt.Printf("B2 já configurado no config.json (bucket %s, chave %s).\n", existing.B2Bucket, maskSecret(existing.B2KeyID))
		if confirmYesNo("Usar essa configuração B2 (só vou testar o acesso)?", true) {
			p.usePresetB2 = true
			p.bucketName = existing.B2Bucket
		}
	}
	if !p.usePresetB2 {
		savedKeyID, savedAppKey, ok := loadMasterEnv(p.masterEnvPath)
		if ok {
			fmt.Printf("Há uma chave master do B2 salva neste servidor (%s, keyID %s).\n", p.masterEnvPath, maskSecret(savedKeyID))
			if confirmYesNo("Usar essa chave master?", true) {
				p.masterKeyID = savedKeyID
				p.masterAppKey = savedAppKey
			} else {
				ok = false
			}
		}
		if !ok {
			fmt.Println("-- Credenciais MASTER do Backblaze B2 (usadas só aqui, para criar o bucket e a chave restrita do cliente) --")
			fmt.Println("   Ficam salvas em", p.masterEnvPath, "— não vai precisar digitar de novo nas próximas instalações.")
			p.masterKeyID = askRequired("keyID master do B2")
			p.masterAppKey = askRequired("applicationKey master do B2")
			if err := saveMasterEnv(p.masterEnvPath, p.masterKeyID, p.masterAppKey); err != nil {
				fmt.Println("  Aviso: não consegui salvar para reuso futuro:", err)
			}
		}
		defBucket := existing.B2Bucket
		if defBucket == "" {
			defBucket = "mgsis-backup-" + sanitizeBucketPart(p.clientID)
		}
		p.bucketName = ask("Nome do bucket B2", defBucket)
	}
	fmt.Println()
	p.workDir = ask("Diretório de trabalho (dumps/logs locais)", existing.WorkDir)
	p.excludeFotos = confirmYesNo("Excluir a tabela de fotos do backup diário (ela tem perfil próprio)?", existing.ExcludeFotos)
	p.noSchedule = !confirmYesNo("Instalar o agendamento automático agora (cron / Tarefa Agendada)?", true)

	fmt.Println()
	return p
}

// maskSecret shows only the first 4 characters of an identifier.
func maskSecret(s string) string {
	if len(s) <= 4 {
		return "****"
	}
	return s[:4] + "****"
}

// provisionClient does the actual work, shared by the flag-driven and
// the interactive front-ends above.
func provisionClient(p setupParams) int {
	if p.bucketName == "" {
		p.bucketName = "mgsis-backup-" + sanitizeBucketPart(p.clientID)
	}

	cfg := p.base
	if cfg.B2Prefix == "" { // flag mode passes a zero-value base
		cfg = DefaultConfig()
	}
	cfg.ClientID = p.clientID
	cfg.PGDatabase = p.pgDatabase
	cfg.PGUser = p.pgUser
	cfg.PGHost = p.pgHost
	cfg.PGPort = p.pgPort
	cfg.PGPassword = p.pgPassword
	cfg.B2Bucket = p.bucketName
	cfg.ExcludeFotos = p.excludeFotos
	if p.workDir != "" {
		cfg.WorkDir = p.workDir
	} else {
		cfg.WorkDir = defaultWorkDir()
	}

	if p.usePresetB2 {
		// config.json already carries the client's restricted B2 key and
		// bucket (put there by whoever prepared the install package), so
		// the support user types nothing: just prove the key works.
		fmt.Println("Testando acesso ao B2 com a chave já configurada...")
		if code := runTesteB2(cfg); code != 0 {
			fmt.Fprintln(os.Stderr, "A chave/bucket do config.json não funcionou; corrija o config.json e rode o setup de novo.")
			return code
		}
	} else {
		fmt.Printf("Autenticando no Backblaze B2 com a chave master...\n")
		master := b2.New(p.masterKeyID, p.masterAppKey)
		if err := master.Authorize(); err != nil {
			fmt.Fprintln(os.Stderr, "Erro ao autenticar no B2:", err)
			return 1
		}

		lifecycleRules := defaultLifecycleRules()

		bucket, err := master.FindBucketByName(p.bucketName)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Erro ao consultar bucket:", err)
			return 1
		}
		if bucket == nil {
			fmt.Printf("Criando bucket %s...\n", p.bucketName)
			bucket, err = master.CreateBucket(p.bucketName, lifecycleRules)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Erro ao criar bucket:", err)
				return 1
			}
		} else {
			fmt.Printf("Bucket %s já existe, atualizando regras de ciclo de vida...\n", p.bucketName)
			if err := master.UpdateBucketLifecycle(bucket.BucketID, lifecycleRules); err != nil {
				fmt.Fprintln(os.Stderr, "Erro ao atualizar regras de ciclo de vida:", err)
				return 1
			}
		}

		fmt.Println("Criando chave de aplicação restrita (sem acesso a outros buckets)...")
		key, err := master.CreateRestrictedKey(bucket.BucketID, "backup-"+p.clientID, cfg.B2Prefix)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Erro ao criar chave restrita:", err)
			return 1
		}
		cfg.B2KeyID = key.ApplicationKeyID
		cfg.B2AppKey = key.ApplicationKey
	}

	cp := p.configPath
	if cp == "" {
		cp = defaultConfigPath()
	}
	if err := SaveConfig(cp, cfg); err != nil {
		fmt.Fprintln(os.Stderr, "Erro ao salvar config.json:", err)
		return 1
	}
	fmt.Printf("Configuração salva em: %s\n", cp)

	if err := os.MkdirAll(cfg.WorkDir, 0700); err != nil {
		fmt.Fprintln(os.Stderr, "Aviso: não consegui criar o diretório de trabalho", cfg.WorkDir, ":", err)
	}

	if p.noSchedule {
		fmt.Println("Agendamento não instalado. Configure manualmente quando quiser.")
		return 0
	}

	exePath, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Aviso: não consegui determinar o caminho do executável, agendamento não instalado:", err)
		return 0
	}

	// The scheduled runs read config.json from beside the executable by
	// default, so only pass --config when it was saved somewhere else.
	cfgArg := ""
	if filepath.Clean(cp) != filepath.Clean(defaultConfigPath()) {
		cfgArg = cp
	}
	if err := installSchedule(exePath, cfgArg); err != nil {
		fmt.Fprintln(os.Stderr, "Aviso: falha ao instalar o agendamento automático:", err)
		fmt.Fprintln(os.Stderr, "Configure manualmente: diario (12:30 e 18:30, seg-sáb), fotos (20:00 qua), verifica (02:00 dom).")
		return 0
	}

	fmt.Println("Agendamento instalado com sucesso.")
	fmt.Println()
	fmt.Println("Instalação concluída para o cliente", p.clientID+".")
	return 0
}

// defaultLifecycleRules mirrors the rules setup_b2_bucket.sh used to
// apply by hand: dumps kept hidden-but-recoverable for 90 days before
// final deletion, across every profile's prefix.
func defaultLifecycleRules() []b2.LifecycleRule {
	days90 := 90
	day1 := 1
	return []b2.LifecycleRule{
		{FileNamePrefix: "backups/diario/", DaysFromUploadingToHiding: &days90, DaysFromHidingToDeleting: &day1},
		{FileNamePrefix: "backups/fotos/", DaysFromUploadingToHiding: &days90, DaysFromHidingToDeleting: &day1},
		{FileNamePrefix: "backups/logs/", DaysFromUploadingToHiding: &days90, DaysFromHidingToDeleting: &day1},
		{FileNamePrefix: "backups/verificacao/", DaysFromUploadingToHiding: &days90, DaysFromHidingToDeleting: &day1},
	}
}

func sanitizeBucketPart(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return b.String()
}

// confirmYesNo prints a yes/no prompt with the given default (shown as
// the capitalized option) and reads the answer from stdin.
func confirmYesNo(prompt string, def bool) bool {
	options := "[S/n]"
	if !def {
		options = "[s/N]"
	}
	fmt.Printf("%s %s: ", prompt, options)
	line, _ := stdinReader.ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	if line == "" {
		return def
	}
	return line == "s" || line == "sim" || line == "y" || line == "yes"
}
