# Gerenciador de Backup MGSIS

Painel web para monitorar backups armazenados em buckets do Backblaze B2.

## Status atual (fase 1)

- Conecta na conta B2 via Application Key
- Lista os buckets da conta
- Mostra KPIs por bucket: último backup, dias sem backup, quantidade de arquivos, tamanho total
- Indicador visual (ok / atenção / crítico) por bucket, conforme limites configuráveis
- Importa uma planilha de clientes (`pessoa_nome`, `pessoa_ruc`) pelo botão **Importar Excel**.
  A lista fica salva em `server/data/clients.json` — só precisa reimportar quando quiser atualizar.
- RUC que está na planilha mas ainda não tem bucket no Backblaze aparece como KPI
  vermelho em destaque (**NÃO FAZENDO BACKUP**)
- Filtro rápido: Todos / Em dia / Em atraso

- Restaura um backup direto num PostgreSQL pelo botão **Restaurar** na lista de arquivos
  (veja "Restauração" abaixo)

Próximas fases: upload de backups pelo painel.

## Restauração

Cada `.tar.gz` do bucket contém um único dump `pg_dump` em SQL texto (gerado no PostgreSQL 11).
Ao clicar em **Restaurar**, o painel pede os dados de conexão do destino e o servidor:

1. cria o banco com o nome do bucket (o RUC do cliente) usando `TEMPLATE template0 ENCODING 'UTF8'`
2. transmite o arquivo do B2 direto para o `psql` (`fetch` → `gunzip` → `tar` → `psql`),
   sem gravar nada em disco
3. acompanha o progresso por um job, consultado pela tela até terminar

Observações:

- O banco de destino **não pode existir**. Se existir, a tela pergunta antes de apagar e recriar.
- `ON_ERROR_STOP` fica desligado de propósito: um dump do PG 11 restaurado num PG 12+ sempre
  gera erros benignos (`default_with_oids`, por exemplo) que não podem abortar a restauração.
  Os avisos aparecem no final, agrupados.
- O `psql` precisa estar instalado na máquina que roda a API. O caminho é detectado
  automaticamente no Windows ou definido em `PSQL_PATH`.
- O host/porta vêm do formulário, então o destino pode ser outra máquina — desde que a API
  alcance esse PostgreSQL pela rede.

### Formato da planilha

Primeira aba, com cabeçalho na primeira linha:

| pessoa_nome        | pessoa_ruc |
| ------------------ | ---------- |
| Nome do cliente    | 80013667   |

Aceita `.xlsx`, `.xls` ou `.csv`. O RUC deve bater exatamente com o nome do bucket no B2.
Se o RUC tiver zeros à esquerda, formate a coluna como texto no Excel.

## Estrutura

- `server/` — API em Node.js + TypeScript (Express) que fala com o B2
- `web/` — painel em React + TypeScript (Vite)

## Configuração

1. Crie uma Application Key no B2 em https://secure.backblaze.com/app_keys.htm
   - Para esta fase, capacidades `listBuckets` e `listFiles` já são suficientes
2. Copie `server/.env.example` para `server/.env` e preencha:
   - `B2_APPLICATION_KEY_ID`
   - `B2_APPLICATION_KEY`
   - Ajuste `KPI_WARNING_DAYS` / `KPI_CRITICAL_DAYS` se quiser outros limites de alerta

## Rodando em desenvolvimento

Na raiz do projeto:

```bash
npm install

# terminal 1
npm run dev:server

# terminal 2
npm run dev:web
```

O painel abre em `http://localhost:5173` e consome a API em `http://localhost:4000` (proxy configurado no Vite).

## Build de produção

```bash
npm run build:server
npm run build:web
```
