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
- Agendamento: diario seg-sáb 12:30 e 18:30, fotos qua 20:00, verifica dom 02:00.

## Comandos
`setup` (wizard interativo sem flags; ou flags), `diario`, `fotos`, `logs`,
`verifica`, `teste-b2`. `--dry-run` simula; `--config` aponta outro config.json.

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
cmd_verifica.go · cmd_testeb2.go · config.go · master_env.go · logging.go ·
lock.go · archive.go · schedule_linux.go / schedule_windows.go (por sufixo de
nome, sem build tags) · b2/client.go.
