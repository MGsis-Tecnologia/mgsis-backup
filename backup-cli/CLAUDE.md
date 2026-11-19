# mgsis-backup — contexto para o Claude

Executável único em Go (só biblioteca padrão, sem módulos de terceiros) que
substitui os scripts `setup_b2_bucket.sh`, `backup_uni.sh` e `backup_verifica.sh`
da MGsis Tecnologia, **somente para instalações novas** (clientes que já rodam
os .sh não são migrados). Faz backup de PostgreSQL para o Backblaze B2.

## Decisões já tomadas (não reverter sem perguntar)
- Única dependência externa: ferramentas cliente do PostgreSQL já instaladas
  (`pg_dump`, `psql`, `createdb`, `dropdb`). Não reimplementar dump em Go.
- Sem rclone, Python ou b2-CLI: `b2/client.go` fala direto com a API nativa do B2
  (inclui upload multipart para arquivos grandes).
- Sem Go modules externos e sem CGO (precisa compilar estático e cruzado).
- `config.json` fica **ao lado do executável**; cron/Tarefas Agendadas NÃO usam
  `--config` (só se o config foi salvo em outro lugar). Comando vem antes das
  flags: `mgsis-backup diario --config x`.
- Resposta do `b2_authorize_account` vem em formato plano (`apiUrl` na raiz) nas
  contas reais; o formato aninhado (`apiInfo.storageApi`) também é aceito.
- Chave master do B2 fica em `~/.config/b2-auto/master.env` (mesmo caminho/formato
  do .sh antigo, chmod 600). O wizard pergunta se usa a salva ou digita outra.
- Se o `config.json` já traz chave B2 + bucket, o `setup` só testa o acesso
  (não pede master, não cria bucket/chave).
- Verificação de integridade: hash SQL por tabela (comutativo, imune à ordem
  física das linhas), com 1 retentativa automática antes de declarar FALHA.
  Não voltar para hash do arquivo de dump (deu falso positivo).
- Agendamento: diario seg-sáb 12:30 e 18:30, fotos qua 20:00, verifica dom 02:00,
  fisico sáb 23:00.
- Ciclo de vida no B2 (aplicado pelo `setup`): diario 7 dias, fotos 30, logs 7,
  fisico 14, verificacao 14 — contados do upload; o arquivo é escondido e apagado
  1 dia depois.
- **Perfil `fisico`** (port do `backup_uni.sh --fisico`): `.tar.gz` do diretório de
  dados, não pg_dump. `physical_method` = `legacy_copy` (padrão histórico; exige
  `physical_source_dir`) ou `pg_basebackup` (`-Fp -Xs -P`, precisa de REPLICATION
  e pg_hba). Campos só validados quando o perfil roda; config sem eles continua
  válido. Tudo no tar fica sob `main/` (a restauração depende desse nome). Local:
  `{work_dir}/fisico/{client}-fisico-DD-MM-YY-HH-MM.tar.gz`, 30 dias; B2:
  `{b2_prefix}/fisico/`. Códigos de saída: 64 método inválido, 69 sem pg_basebackup.
- `legacy_copy` grava direto no tar.gz (sem cópia intermediária), preserva modo,
  uid/gid e symlinks (pg_wal/pg_tblspc), pula sockets/especiais; arquivo que some
  durante a leitura = WARN e segue; arquivo que diminui é completado com zeros
  (como o GNU tar); outro erro de leitura aborta.
- O físico cobre o servidor inteiro, então roda **uma vez** (não por banco) e vai
  para o bucket de `physical_client_id` (padrão: primeiro banco; `--banco` escolhe).
- O crontab original não tinha entrada para o físico; decidido (Rogerio) agendar
  sábado 23:00, só quando `ValidatePhysical()` passa (senão falharia toda semana;
  no Windows a tarefa `MgsisBackupFisico` antiga é removida).
  verificacao fica em 14 (não 7) porque roda semanal: com 7, um domingo perdido
  deixaria o painel sem nenhum resultado.
- **Vários bancos por servidor** (até 3 hoje): um banco = um cliente = um bucket
  com sua própria chave restrita. No `config.json`, conexão PostgreSQL (usuário,
  host, porta, senha), `work_dir` e retenção ficam no topo, uma vez só; o que
  muda por banco fica na lista `bancos` (`client_id`, `pg_database`, `b2_bucket`,
  `b2_key_id`, `b2_application_key`). `client_id`, `pg_database` e `b2_bucket`
  não podem se repetir (o `client_id` prefixa os arquivos no `work_dir` comum).
- Cada comando roda para todos os bancos em sequência (nunca em paralelo); falha
  num banco é registrada e os demais continuam; código de saída 1 se algum
  falhou. Agendamento continua com uma entrada por rotina (não uma por banco).
  `--banco <client_id>` roda só um.
- `config.json` antigo (client_id/pg_database/b2_* no topo, um banco só) continua
  sendo lido: `normalize()` converte para a lista `bancos`; ao salvar, só a lista
  é gravada. `Config.Targets()` devolve uma cópia por banco com esses campos no
  topo — é o que o código de backup/verificação/B2 recebe.
- Erros dentro do loop são retornados (`error`), não `logger.Fatal`/`os.Exit`,
  para não matar os bancos seguintes nem deixar o lock para trás.

## Comandos
`setup` (wizard interativo sem flags — pergunta a conexão uma vez e depois
quantos bancos; ou flags: `--banco CLIENTE:BANCO[:BUCKET]` repetido, ou o atalho
`--client-id`/`--pg-database` para um banco só; `--physical-method`,
`--physical-source-dir`, `--physical-client-id`), `diario`, `fotos`, `logs`,
`fisico`, `verifica`, `teste-b2`. `--dry-run` simula; `--config` aponta outro config.json;
`--banco <client_id>` limita a um banco.

## Build (3 binários)
`./build.sh` (Linux/macOS) ou `.\build.ps1` (Windows). Gera em `dist/`:
- `mgsis-backup-linux-amd64`  (servidores x86_64)
- `mgsis-backup-linux-386`    (servidores 32 bits, `uname -m` = i686; estático)
- `mgsis-backup-windows-amd64.exe`
Conferir com `go vet ./...` antes. Testar sempre `diario --dry-run` com um
config.json de exemplo. O ambiente de desenvolvimento pode não alcançar a
Backblaze: o teste real de B2 só roda no servidor do cliente.

## Arquivos
main.go (CLI) · cmd_setup.go (wizard/provisionamento) · cmd_backup.go ·
cmd_fisico.go · diskfree_linux.go / diskfree_windows.go ·
cmd_verifica.go · cmd_testeb2.go · config.go · master_env.go · logging.go ·
lock.go · archive.go · schedule_linux.go / schedule_windows.go (por sufixo de
nome, sem build tags) · b2/client.go.
