import { useEffect, useMemo, useRef, useState } from "react";
import { fetchBuckets, fetchClients, importClients } from "./api.js";
import type { BucketSummary, ClientStore } from "./types.js";
import { BucketCard } from "./components/BucketCard.js";
import { BucketTable } from "./components/BucketTable.js";
import { FileListModal } from "./components/FileListModal.js";

type Filter = "todos" | "emDia" | "emAtraso";
type View = "cards" | "lista";
type SortKey = "ultimoBackup" | "nome" | "ruc" | "tamanho" | "arquivos";
type SortDir = "asc" | "desc";

const filterOptions: { key: Filter; label: string }[] = [
  { key: "todos", label: "Todos" },
  { key: "emDia", label: "Em dia" },
  { key: "emAtraso", label: "Em atraso" },
];

const sortOptions: { key: SortKey; label: string }[] = [
  { key: "ultimoBackup", label: "Último backup" },
  { key: "nome", label: "Nome do cliente" },
  { key: "ruc", label: "RUC / bucket" },
  { key: "tamanho", label: "Tamanho" },
  { key: "arquivos", label: "Quantidade de arquivos" },
];

const VIEW_STORAGE_KEY = "mgsis-backup-view";

function loadStoredView(): View {
  try {
    const stored = localStorage.getItem(VIEW_STORAGE_KEY);
    return stored === "lista" ? "lista" : "cards";
  } catch {
    return "cards";
  }
}

function matchesFilter(bucket: BucketSummary, filter: Filter): boolean {
  if (filter === "todos") return true;
  if (filter === "emDia") return bucket.status === "ok";
  return bucket.status !== "ok"; // em atraso: warning, critical, empty e não fazendo backup
}

function stripAccents(value: string): string {
  return value.normalize("NFD").replace(/\p{Diacritic}/gu, "");
}

function matchesSearch(bucket: BucketSummary, query: string): boolean {
  if (!query.trim()) return true;
  const needle = stripAccents(query.trim().toLowerCase());
  return [bucket.bucketName, bucket.clientName ?? ""].some((field) =>
    stripAccents(field.toLowerCase()).includes(needle),
  );
}

function sortValue(bucket: BucketSummary, key: SortKey): number | string {
  switch (key) {
    case "ultimoBackup":
      return bucket.lastBackupAt ? Date.parse(bucket.lastBackupAt) : -Infinity;
    case "nome":
      return bucket.clientName || bucket.bucketName;
    case "ruc":
      return bucket.bucketName;
    case "tamanho":
      return bucket.totalSizeBytes;
    case "arquivos":
      return bucket.fileCount;
  }
}

function sortBuckets(list: BucketSummary[], key: SortKey, dir: SortDir): BucketSummary[] {
  const sign = dir === "asc" ? 1 : -1;
  return [...list].sort((a, b) => {
    const va = sortValue(a, key);
    const vb = sortValue(b, key);
    const cmp =
      typeof va === "number" && typeof vb === "number"
        ? va - vb
        : String(va).localeCompare(String(vb), undefined, { numeric: true, sensitivity: "base" });
    return cmp * sign;
  });
}

export default function App() {
  const [buckets, setBuckets] = useState<BucketSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [selectedBucket, setSelectedBucket] = useState<BucketSummary | null>(null);
  const [filter, setFilter] = useState<Filter>("todos");
  const [search, setSearch] = useState("");
  const [sortKey, setSortKey] = useState<SortKey>("ruc");
  const [sortDir, setSortDir] = useState<SortDir>("asc");
  const [view, setView] = useState<View>(loadStoredView);

  function changeView(next: View) {
    setView(next);
    try {
      localStorage.setItem(VIEW_STORAGE_KEY, next);
    } catch {
      // ignore (private browsing, etc.)
    }
  }

  const [clientStore, setClientStore] = useState<ClientStore | null>(null);
  const [importing, setImporting] = useState(false);
  const [importMsg, setImportMsg] = useState<string | null>(null);
  const [importError, setImportError] = useState<string | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  async function load(refresh = false) {
    setLoading(true);
    setError(null);
    try {
      const data = await fetchBuckets(refresh);
      setBuckets(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Erro desconhecido");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    load();
    fetchClients()
      .then(setClientStore)
      .catch(() => {});
  }, []);

  async function handleImportFile(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    e.target.value = "";
    if (!file) return;
    setImporting(true);
    setImportMsg(null);
    setImportError(null);
    try {
      const store = await importClients(file);
      setClientStore(store);
      setImportMsg(`${store.clients.length} clientes importados.`);
      await load(true);
    } catch (err) {
      setImportError(err instanceof Error ? err.message : "Erro ao importar a planilha");
    } finally {
      setImporting(false);
    }
  }

  const searchedBuckets = useMemo(
    () => buckets.filter((b) => matchesSearch(b, search)),
    [buckets, search],
  );

  const counts = useMemo(() => {
    const missing = searchedBuckets.filter((b) => b.status === "missing").length;
    const emDia = searchedBuckets.filter((b) => b.status === "ok").length;
    return { missing, emDia, emAtraso: searchedBuckets.length - emDia, total: searchedBuckets.length };
  }, [searchedBuckets]);

  const visibleBuckets = useMemo(() => {
    const filtered = searchedBuckets.filter((b) => matchesFilter(b, filter));
    return sortBuckets(filtered, sortKey, sortDir);
  }, [searchedBuckets, filter, sortKey, sortDir]);

  return (
    <div className="app">
      <header className="app-header">
        <h1>Gerenciador de Backup MGSIS</h1>
        <div className="app-actions">
          <input
            ref={fileInputRef}
            type="file"
            accept=".xlsx,.xls,.csv"
            hidden
            onChange={handleImportFile}
          />
          <button onClick={() => fileInputRef.current?.click()} disabled={importing}>
            {importing ? "Importando…" : "Importar Excel"}
          </button>
          <button onClick={() => load(true)} disabled={loading}>
            {loading ? "Atualizando…" : "Atualizar"}
          </button>
        </div>
      </header>

      {importMsg && <p className="import-msg">{importMsg}</p>}
      {importError && <p className="error">{importError}</p>}
      {clientStore?.updatedAt && (
        <p className="muted-line">
          Lista de clientes importada em {new Date(clientStore.updatedAt).toLocaleString("pt-BR")} ·{" "}
          {clientStore.clients.length} clientes
        </p>
      )}

      {error && <p className="error">{error}</p>}

      {!error && buckets.length === 0 && !loading && <p>Nenhum bucket encontrado.</p>}

      {buckets.length > 0 && (
        <div className="toolbar">
          <input
            type="search"
            className="search-input"
            placeholder="Buscar por bucket, cliente ou RUC…"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            aria-label="Buscar bucket, cliente ou RUC"
          />
          <div className="sort-control">
            <label htmlFor="sort-key">Ordenar por</label>
            <select
              id="sort-key"
              value={sortKey}
              onChange={(e) => setSortKey(e.target.value as SortKey)}
            >
              {sortOptions.map((opt) => (
                <option key={opt.key} value={opt.key}>
                  {opt.label}
                </option>
              ))}
            </select>
            <button
              type="button"
              className="sort-dir-btn"
              onClick={() => setSortDir((d) => (d === "asc" ? "desc" : "asc"))}
              title={sortDir === "asc" ? "Ordem crescente" : "Ordem decrescente"}
              aria-label={sortDir === "asc" ? "Ordem crescente" : "Ordem decrescente"}
            >
              {sortDir === "asc" ? "↑" : "↓"}
            </button>
          </div>
        </div>
      )}

      {buckets.length > 0 && (
        <div className="filter-bar">
          {filterOptions.map((opt) => (
            <button
              key={opt.key}
              type="button"
              className={`filter-chip${filter === opt.key ? " active" : ""}`}
              onClick={() => setFilter(opt.key)}
            >
              {opt.label}
              {opt.key === "emDia" && ` (${counts.emDia})`}
              {opt.key === "emAtraso" && ` (${counts.emAtraso})`}
              {opt.key === "todos" && ` (${counts.total})`}
            </button>
          ))}
          {counts.missing > 0 && (
            <span className="missing-hint">{counts.missing} não fazendo backup</span>
          )}

          <div className="view-toggle" role="group" aria-label="Modo de visualização">
            <button
              type="button"
              className={`view-toggle-btn${view === "cards" ? " active" : ""}`}
              onClick={() => changeView("cards")}
              title="Ver como cards"
            >
              Cards
            </button>
            <button
              type="button"
              className={`view-toggle-btn${view === "lista" ? " active" : ""}`}
              onClick={() => changeView("lista")}
              title="Ver como lista"
            >
              Lista
            </button>
          </div>
        </div>
      )}

      {view === "cards" ? (
        <div className="bucket-grid">
          {visibleBuckets.map((bucket) => (
            <BucketCard
              key={bucket.bucketId}
              bucket={bucket}
              onClick={() => bucket.status !== "missing" && setSelectedBucket(bucket)}
            />
          ))}
        </div>
      ) : (
        <BucketTable
          buckets={visibleBuckets}
          onSelect={(bucket) => bucket.status !== "missing" && setSelectedBucket(bucket)}
        />
      )}

      {buckets.length > 0 && visibleBuckets.length === 0 && (
        <p className="muted-line">Nenhum bucket encontrado com esse filtro/busca.</p>
      )}

      {selectedBucket && (
        <FileListModal
          bucketId={selectedBucket.bucketId}
          bucketName={selectedBucket.bucketName}
          onClose={() => setSelectedBucket(null)}
        />
      )}
    </div>
  );
}
