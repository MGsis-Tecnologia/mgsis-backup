package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"mgsis-backup/b2"
)

// stdinReader is shared by every prompt so buffered input is never lost
// between questions (matters when answers are piped in).
var stdinReader = bufio.NewReader(os.Stdin)

// maxBancos caps how many databases one server can be set up with — a
// sanity limit against typos in the "how many" prompt, not a technical one.
const maxBancos = 10

// setupBanco is one database being provisioned: one client, one bucket,
// one restricted B2 key.
type setupBanco struct {
	clientID   string
	pgDatabase string
	bucketName string

	// usePreset means config.json already carries a working key for this
	// bucket: skip master-key provisioning and just verify access.
	usePreset    bool
	presetKeyID  string
	presetAppKey string
}

// setupParams holds everything provisionClient needs, regardless of
// whether it came from command-line flags or from the interactive
// wizard below.
type setupParams struct {
	bancos []setupBanco

	// Shared by every database on this server.
	pgUser     string
	pgHost     string
	pgPort     string
	pgPassword string

	masterKeyID   string
	masterAppKey  string
	masterEnvPath string
	workDir       string
	configPath    string
	excludeFotos  bool
	noSchedule    bool

	// Physical backup (fisico profile); empty source dir with legacy_copy
	// simply leaves fisico unconfigured — the other profiles don't need it.
	physicalMethod    string
	physicalSourceDir string
	physicalClientID  string

	// base is the starting Config (an existing config.json when found,
	// otherwise defaults), so fields the wizard doesn't ask about —
	// retention days, B2 prefix — are preserved instead of reset.
	base Config
}

// bancoFlag collects repeated --banco CLIENTE:BANCO[:BUCKET] values.
type bancoFlag []setupBanco

func (b *bancoFlag) String() string { return fmt.Sprint(len(*b), " banco(s)") }

func (b *bancoFlag) Set(v string) error {
	parts := strings.Split(v, ":")
	if len(parts) < 2 || len(parts) > 3 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("use CLIENTE:BANCO ou CLIENTE:BANCO:BUCKET (recebi %q)", v)
	}
	sb := setupBanco{clientID: parts[0], pgDatabase: parts[1]}
	if len(parts) == 3 {
		sb.bucketName = parts[2]
	}
	*b = append(*b, sb)
	return nil
}

// runSetup provisions the databases of one server: for each one it
// creates (or reuses) the B2 bucket and its lifecycle rules and mints a
// restricted application key scoped to that bucket; then it writes a
// single config.json beside the executable and installs the scheduled
// runs (cron on Linux, Scheduled Tasks on Windows) — one entry per
// profile, which backs up every database in turn. This replaces
// setup_b2_bucket.sh — but, per the agreed scope, only for new
// installations; it is never run against a client already on the old
// .sh-based system.
//
// With no arguments at all ("mgsis-backup setup"), it falls back to the
// old interactive style — one question at a time, Enter to accept the
// default — instead of requiring every flag up front. Pass any flag
// (e.g. --banco ...) to use the non-interactive, flag-driven mode
// instead, useful for scripting the install across many clients.
func runSetup(args []string) int {
	if len(args) == 0 {
		return provisionClient(readSetupParamsInteractive())
	}

	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	var bancos bancoFlag
	fs.Var(&bancos, "banco", "Banco a fazer backup, no formato CLIENTE:BANCO ou CLIENTE:BANCO:BUCKET. Repita para cada banco do servidor (ex: --banco 001234:loja1 --banco 005678:loja2)")
	clientID := fs.String("client-id", "", "Atalho para um único banco: identificador do cliente, ex: 001234 (use junto com --pg-database)")
	pgDatabase := fs.String("pg-database", "", "Atalho para um único banco: nome do banco PostgreSQL")
	bucketName := fs.String("bucket-name", "", "Atalho para um único banco: nome do bucket B2 (padrão: mgsis-backup-<client-id>)")
	pgUser := fs.String("pg-user", "postgres", "Usuário PostgreSQL (o mesmo para todos os bancos)")
	pgHost := fs.String("pg-host", "localhost", "Host PostgreSQL")
	pgPort := fs.String("pg-port", "5432", "Porta PostgreSQL")
	pgPassword := fs.String("pg-password", "", "Senha PostgreSQL (opcional, vazio se usar .pgpass/trust)")
	masterKeyID := fs.String("master-key-id", "", "keyID da chave master do B2 (só precisa na 1ª instalação deste servidor; depois fica salva localmente)")
	masterAppKey := fs.String("master-app-key", "", "applicationKey master do B2 (idem — só na 1ª vez)")
	masterEnvPath := fs.String("master-env", "", "Onde a chave master fica salva localmente (padrão: ~/.config/b2-auto/master.env, o mesmo caminho do setup_b2_bucket.sh antigo)")
	workDir := fs.String("work-dir", "", "Diretório de trabalho para dumps/logs locais (padrão: /dados/backup no Linux, pasta do .exe no Windows)")
	configPath := fs.String("config", "", "Onde salvar o config.json (padrão: ao lado do executável)")
	excludeFotos := fs.Bool("exclude-fotos", true, "Excluir a tabela de fotos do perfil diário (fotos tem perfil próprio)")
	noSchedule := fs.Bool("no-schedule", false, "Não instalar o agendamento (cron/Tarefa Agendada) — só gerar o config.json")
	physicalMethod := fs.String("physical-method", physicalLegacyCopy, "Método do backup físico: legacy_copy ou pg_basebackup")
	physicalSourceDir := fs.String("physical-source-dir", "", "Diretório de dados do PostgreSQL para o backup físico (ex.: /var/lib/postgresql/11/main)")
	physicalClientID := fs.String("physical-client-id", "", "client_id cujo bucket recebe o backup físico (padrão: o primeiro banco)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *clientID != "" || *pgDatabase != "" {
		if len(bancos) > 0 {
			fmt.Fprintln(os.Stderr, "Erro: use --banco (um por banco) ou --client-id/--pg-database (um banco só), não os dois.")
			return 2
		}
		if *clientID == "" || *pgDatabase == "" {
			fmt.Fprintln(os.Stderr, "Erro: --client-id e --pg-database precisam vir juntos.")
			return 2
		}
		bancos = bancoFlag{{clientID: *clientID, pgDatabase: *pgDatabase, bucketName: *bucketName}}
	}

	if len(bancos) == 0 {
		fmt.Fprintln(os.Stderr, "Erro: informe ao menos um banco com --banco CLIENTE:BANCO (repita para cada banco do servidor).")
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
		bancos:        bancos,
		pgUser:        *pgUser,
		pgHost:        *pgHost,
		pgPort:        *pgPort,
		pgPassword:    *pgPassword,
		masterKeyID:   mKeyID,
		masterAppKey:  mAppKey,
		masterEnvPath: envPath,
		workDir:       *workDir,
		configPath:    *configPath,
		excludeFotos:  *excludeFotos,
		noSchedule:    *noSchedule,

		physicalMethod:    *physicalMethod,
		physicalSourceDir: *physicalSourceDir,
		physicalClientID:  *physicalClientID,
	})
}

// readSetupParamsInteractive asks one question at a time, exactly like
// the old setup_b2_bucket.sh prompts: shows the default in brackets,
// Enter alone accepts it. The PostgreSQL connection is asked once (it is
// the same for every database on the server); then, for each database,
// only its client id, database name and bucket.
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
	askDefault := func(label, def string) string {
		if def == "" {
			return askRequired(label)
		}
		return ask(label, def)
	}
	askInt := func(label string, def, min, max int) int {
		for {
			v := ask(label, strconv.Itoa(def))
			n, err := strconv.Atoi(v)
			if err == nil && n >= min && n <= max {
				return n
			}
			fmt.Printf("  (digite um número de %d a %d)\n", min, max)
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

	fmt.Println("-- Conexão PostgreSQL (a mesma para todos os bancos deste servidor) --")
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

	defCount := len(existing.Databases)
	if defCount < 1 {
		defCount = 1
	}
	n := askInt("Quantos bancos (clientes) fazem backup neste servidor?", defCount, 1, maxBancos)

	usedClient := map[string]bool{}
	usedDB := map[string]bool{}
	usedBucket := map[string]bool{}
	for i := 0; i < n; i++ {
		var prev DatabaseEntry
		if i < len(existing.Databases) {
			prev = existing.Databases[i]
		}
		fmt.Println()
		fmt.Printf("-- Banco %d de %d --\n", i+1, n)

		var sb setupBanco
		for {
			sb.clientID = askDefault("Identificador do cliente (ex: 001234)", prev.ClientID)
			if !usedClient[sb.clientID] {
				break
			}
			fmt.Println("  (esse cliente já foi informado em outro banco)")
			prev.ClientID = ""
		}
		for {
			sb.pgDatabase = askDefault("Nome do banco PostgreSQL", prev.PGDatabase)
			if !usedDB[sb.pgDatabase] {
				break
			}
			fmt.Println("  (esse banco já foi informado)")
			prev.PGDatabase = ""
		}

		if prev.B2KeyID != "" && prev.B2AppKey != "" && prev.B2Bucket != "" && !usedBucket[prev.B2Bucket] {
			fmt.Printf("B2 já configurado para este banco no config.json (bucket %s, chave %s).\n", prev.B2Bucket, maskSecret(prev.B2KeyID))
			if confirmYesNo("Usar essa configuração B2 (só vou testar o acesso)?", true) {
				sb.usePreset = true
				sb.bucketName = prev.B2Bucket
				sb.presetKeyID = prev.B2KeyID
				sb.presetAppKey = prev.B2AppKey
			}
		}
		if !sb.usePreset {
			defBucket := prev.B2Bucket
			if defBucket == "" || usedBucket[defBucket] {
				defBucket = "mgsis-backup-" + sanitizeBucketPart(sb.clientID)
			}
			for {
				sb.bucketName = ask("Nome do bucket B2", defBucket)
				if !usedBucket[sb.bucketName] {
					break
				}
				fmt.Println("  (esse bucket já é de outro banco; cada banco precisa do seu)")
			}
		}

		usedClient[sb.clientID] = true
		usedDB[sb.pgDatabase] = true
		usedBucket[sb.bucketName] = true
		p.bancos = append(p.bancos, sb)
	}
	fmt.Println()

	// The master key is only needed to create buckets/keys — skip the
	// question entirely when every database already has a working key.
	needMaster := false
	for _, sb := range p.bancos {
		if !sb.usePreset {
			needMaster = true
		}
	}
	p.masterEnvPath = defaultMasterEnvPath()
	if needMaster {
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
			fmt.Println("-- Credenciais MASTER do Backblaze B2 (usadas só aqui, para criar os buckets e as chaves restritas) --")
			fmt.Println("   Ficam salvas em", p.masterEnvPath, "— não vai precisar digitar de novo nas próximas instalações.")
			p.masterKeyID = askRequired("keyID master do B2")
			p.masterAppKey = askRequired("applicationKey master do B2")
			if err := saveMasterEnv(p.masterEnvPath, p.masterKeyID, p.masterAppKey); err != nil {
				fmt.Println("  Aviso: não consegui salvar para reuso futuro:", err)
			}
		}
		fmt.Println()
	}

	fmt.Println("-- Backup físico (cópia do diretório de dados do PostgreSQL; cobre todos os bancos do servidor) --")
	defMethod := existing.physicalMethod()
	for {
		p.physicalMethod = ask("Método do backup físico (legacy_copy / pg_basebackup)", defMethod)
		if p.physicalMethod == physicalLegacyCopy || p.physicalMethod == physicalPgBasebackup {
			break
		}
		fmt.Println("  (digite legacy_copy ou pg_basebackup)")
	}
	if p.physicalMethod == physicalPgBasebackup {
		fmt.Println("  pg_basebackup exige usuário com permissão REPLICATION e entrada de replicação no pg_hba.conf.")
	}
	defDataDir := existing.PhysicalSourceDir
	if defDataDir == "" {
		probe := existing
		probe.PGUser, probe.PGHost, probe.PGPort, probe.PGPassword = p.pgUser, p.pgHost, p.pgPort, p.pgPassword
		if dir := detectDataDirectory(probe, p.bancos[0].pgDatabase); dir != "" {
			fmt.Println("  Diretório de dados detectado no PostgreSQL:", dir)
			defDataDir = dir
		}
	}
	label := "Diretório de dados do PostgreSQL (vazio = não configurar o backup físico)"
	if p.physicalMethod == physicalPgBasebackup {
		label = "Diretório de dados do PostgreSQL (não é usado pelo pg_basebackup; só fica anotado caso volte para legacy_copy)"
	}
	p.physicalSourceDir = ask(label, defDataDir)
	if len(p.bancos) > 1 {
		defPhys := existing.PhysicalClientID
		if !usedClient[defPhys] {
			defPhys = p.bancos[0].clientID
		}
		for {
			p.physicalClientID = ask("Bucket de qual cliente recebe o backup físico (client_id)", defPhys)
			if usedClient[p.physicalClientID] {
				break
			}
			fmt.Println("  (informe o client_id de um dos bancos acima)")
		}
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
	if len(p.bancos) == 0 {
		fmt.Fprintln(os.Stderr, "Erro: nenhum banco informado.")
		return 2
	}
	for i := range p.bancos {
		if p.bancos[i].bucketName == "" {
			p.bancos[i].bucketName = "mgsis-backup-" + sanitizeBucketPart(p.bancos[i].clientID)
		}
	}

	cfg := p.base
	if cfg.B2Prefix == "" { // flag mode passes a zero-value base
		cfg = DefaultConfig()
	}
	cfg.PGUser = p.pgUser
	cfg.PGHost = p.pgHost
	cfg.PGPort = p.pgPort
	cfg.PGPassword = p.pgPassword
	cfg.ExcludeFotos = p.excludeFotos
	cfg.PhysicalMethod = p.physicalMethod
	cfg.PhysicalSourceDir = p.physicalSourceDir
	cfg.PhysicalClientID = p.physicalClientID
	if cfg.PhysicalMethod != "" && cfg.PhysicalMethod != physicalLegacyCopy && cfg.PhysicalMethod != physicalPgBasebackup {
		fmt.Fprintf(os.Stderr, "Erro: método físico inválido %q (use %s ou %s).\n", cfg.PhysicalMethod, physicalLegacyCopy, physicalPgBasebackup)
		return exitUsage
	}
	if p.workDir != "" {
		cfg.WorkDir = p.workDir
	} else {
		cfg.WorkDir = defaultWorkDir()
	}

	// Catch duplicates before touching B2 (the interactive wizard already
	// prevents them; flag mode does not).
	cfg.Databases = nil
	for _, sb := range p.bancos {
		cfg.Databases = append(cfg.Databases, DatabaseEntry{ClientID: sb.clientID, PGDatabase: sb.pgDatabase, B2Bucket: sb.bucketName, B2KeyID: "-", B2AppKey: "-"})
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, "Erro:", err)
		return 2
	}

	var master *b2.Client
	lifecycleRules := defaultLifecycleRules()

	cfg.Databases = nil
	for i, sb := range p.bancos {
		fmt.Printf("== Banco %d de %d: cliente %s, banco %s, bucket %s ==\n", i+1, len(p.bancos), sb.clientID, sb.pgDatabase, sb.bucketName)
		entry := DatabaseEntry{ClientID: sb.clientID, PGDatabase: sb.pgDatabase, B2Bucket: sb.bucketName}

		if sb.usePreset {
			// config.json already carries this bucket's restricted key
			// (put there by whoever prepared the install package), so the
			// support user types nothing: just prove the key works.
			entry.B2KeyID = sb.presetKeyID
			entry.B2AppKey = sb.presetAppKey
			t := cfg
			t.ClientID, t.PGDatabase, t.B2Bucket, t.B2KeyID, t.B2AppKey = entry.ClientID, entry.PGDatabase, entry.B2Bucket, entry.B2KeyID, entry.B2AppKey
			fmt.Println("Testando acesso ao B2 com a chave já configurada...")
			if code := runTesteB2(t); code != 0 {
				fmt.Fprintln(os.Stderr, "A chave/bucket deste banco no config.json não funcionou; corrija o config.json e rode o setup de novo.")
				fmt.Fprintln(os.Stderr, "Nada foi salvo.")
				return code
			}
		} else {
			if master == nil {
				fmt.Println("Autenticando no Backblaze B2 com a chave master...")
				master = b2.New(p.masterKeyID, p.masterAppKey)
				if err := master.Authorize(); err != nil {
					fmt.Fprintln(os.Stderr, "Erro ao autenticar no B2:", err)
					return 1
				}
			}
			keyID, appKey, err := provisionBucket(master, sb, cfg.B2Prefix, lifecycleRules)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Erro:", err)
				fmt.Fprintln(os.Stderr, "Nada foi salvo. Corrija o problema e rode o setup de novo (buckets já criados são reaproveitados).")
				return 1
			}
			entry.B2KeyID = keyID
			entry.B2AppKey = appKey
		}
		cfg.Databases = append(cfg.Databases, entry)
		fmt.Println()
	}

	cp := p.configPath
	if cp == "" {
		cp = defaultConfigPath()
	}
	if err := SaveConfig(cp, cfg); err != nil {
		fmt.Fprintln(os.Stderr, "Erro ao salvar config.json:", err)
		return 1
	}
	fmt.Printf("Configuração salva em: %s (%d banco(s))\n", cp, len(cfg.Databases))

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
	// One entry per profile covers every database: the executable loops
	// over all of them in config.json.
	cfgArg := ""
	if filepath.Clean(cp) != filepath.Clean(defaultConfigPath()) {
		cfgArg = cp
	}
	// Only schedule fisico when it can actually run; otherwise it would
	// fail every Saturday.
	withFisico := cfg.ValidatePhysical() == nil
	if err := installSchedule(exePath, cfgArg, withFisico); err != nil {
		fmt.Fprintln(os.Stderr, "Aviso: falha ao instalar o agendamento automático:", err)
		fmt.Fprintln(os.Stderr, "Configure manualmente: diario (12:30 e 18:30, seg-sáb), fotos (20:00 qua), verifica (02:00 dom), fisico (23:00 sáb).")
		return 0
	}

	fmt.Println("Agendamento instalado com sucesso (cada rotina faz o backup de todos os bancos).")
	if withFisico {
		fmt.Println("Backup físico agendado para sábado 23:00.")
	} else if err := cfg.ValidatePhysical(); err != nil {
		fmt.Println("Backup físico NÃO agendado:", err)
	}
	fmt.Println()
	ids := make([]string, 0, len(cfg.Databases))
	for _, d := range cfg.Databases {
		ids = append(ids, d.ClientID)
	}
	fmt.Println("Instalação concluída para:", strings.Join(ids, ", ")+".")
	return 0
}

// provisionBucket creates (or reuses) one database's bucket, applies the
// lifecycle rules and mints a restricted key that can only reach it.
func provisionBucket(master *b2.Client, sb setupBanco, prefix string, rules []b2.LifecycleRule) (keyID, appKey string, err error) {
	bucket, err := master.FindBucketByName(sb.bucketName)
	if err != nil {
		return "", "", fmt.Errorf("ao consultar o bucket %s: %w", sb.bucketName, err)
	}
	if bucket == nil {
		fmt.Printf("Criando bucket %s...\n", sb.bucketName)
		bucket, err = master.CreateBucket(sb.bucketName, rules)
		if err != nil {
			return "", "", fmt.Errorf("ao criar o bucket %s: %w", sb.bucketName, err)
		}
	} else {
		fmt.Printf("Bucket %s já existe, atualizando regras de ciclo de vida...\n", sb.bucketName)
		if err := master.UpdateBucketLifecycle(bucket.BucketID, rules); err != nil {
			return "", "", fmt.Errorf("ao atualizar regras de ciclo de vida de %s: %w", sb.bucketName, err)
		}
	}

	fmt.Println("Criando chave de aplicação restrita (sem acesso a outros buckets)...")
	key, err := master.CreateRestrictedKey(bucket.BucketID, "backup-"+sb.clientID, prefix)
	if err != nil {
		return "", "", fmt.Errorf("ao criar a chave restrita de %s: %w", sb.bucketName, err)
	}
	return key.ApplicationKeyID, key.ApplicationKey, nil
}

// detectDataDirectory asks the running PostgreSQL where its data
// directory is (needs superuser or pg_read_all_settings). Any failure
// just means no suggestion.
func detectDataDirectory(cfg Config, dbName string) string {
	out, err := runPsqlQuery(cfg, dbName, "SHOW data_directory;")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// defaultLifecycleRules sets how long each profile's files stay in B2:
// diario 7 days, fotos 30, logs 7, fisico 14, verificacao 14 — counted from upload,
// after which B2 hides the file and deletes it for good 1 day later.
// verificacao keeps 14 (not 7) because it runs weekly: with 7, a single
// missed Sunday run would leave the panel with no result at all.
func defaultLifecycleRules() []b2.LifecycleRule {
	days7 := 7
	days14 := 14
	days30 := 30
	day1 := 1
	return []b2.LifecycleRule{
		{FileNamePrefix: "backups/diario/", DaysFromUploadingToHiding: &days7, DaysFromHidingToDeleting: &day1},
		{FileNamePrefix: "backups/fotos/", DaysFromUploadingToHiding: &days30, DaysFromHidingToDeleting: &day1},
		{FileNamePrefix: "backups/logs/", DaysFromUploadingToHiding: &days7, DaysFromHidingToDeleting: &day1},
		{FileNamePrefix: "backups/fisico/", DaysFromUploadingToHiding: &days14, DaysFromHidingToDeleting: &day1},
		{FileNamePrefix: "backups/verificacao/", DaysFromUploadingToHiding: &days14, DaysFromHidingToDeleting: &day1},
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
