import { useEffect, useMemo, useState } from "react";
import { fetchBuckets } from "./api.js";
import type { BucketSummary } from "./types.js";
import { BucketCard } from "./components/BucketCard.js";
import { BucketTable } from "./components/BucketTable.js";
import { FileListModal } from "./components/FileListModal.js";
import { EmailTestModal } from "./components/EmailTestModal.js";
import { WsConfigModal } from "./components/WsConfigModal.js";

type Filter = "todos" | "emDia" | "emAtraso";
type CountryFilter = "todos" | "BR" | "PY";
type View = "cards" | "lista";
type SortKey = "ultimoBackup" | "nome" | "ruc" | "tamanho" | "arquivos";
type SortDir = "asc" | "desc";

const filterOptions: { key: Filter; label: string }[] = [
  { key: "todos", label: "Todos" },
  { key: "emDia", label: "Em dia" },
  { key: "emAtraso", label: "Em atraso" },
];

const countryOptions: { key: CountryFilter; label: string }[] = [
  { key: "todos", label: "Todos" },
  { key: "BR", label: "Brasil" },
  { key: "PY", label: "Paraguai" },
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

function matchesCountry(bucket: BucketSummary, filter: CountryFilter): boolean {
  return filter === "todos" || bucket.country === filter;
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
  const [countryFilter, setCountryFilter] = useState<CountryFilter>("todos");
  const [clientsError, setClientsError] = useState<string | null>(null);
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

  const [emailModalOpen, setEmailModalOpen] = useState(false);
  const [wsModalOpen, setWsModalOpen] = useState(false);

  async function load(refresh = false) {
    setLoading(true);
    setError(null);
    try {
      const data = await fetchBuckets(refresh);
      setBuckets(data.buckets);
      setClientsError(data.clientsError);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Erro desconhecido");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    load();
  }, []);

  const searchedBuckets = useMemo(
    () => buckets.filter((b) => matchesSearch(b, search)),
    [buckets, search],
  );

  // Contagem por status respeita o país selecionado; a de país mostra sempre o total do país
  const counts = useMemo(() => {
    const inCountry = searchedBuckets.filter((b) => matchesCountry(b, countryFilter));
    const missing = inCountry.filter((b) => b.status === "missing").length;
    const emDia = inCountry.filter((b) => b.status === "ok").length;
    return { missing, emDia, emAtraso: inCountry.length - emDia, total: inCountry.length };
  }, [searchedBuckets, countryFilter]);

  const countryCounts = useMemo(() => {
    const br = searchedBuckets.filter((b) => b.country === "BR").length;
    const py = searchedBuckets.filter((b) => b.country === "PY").length;
    return { todos: searchedBuckets.length, BR: br, PY: py };
  }, [searchedBuckets]);

  const visibleBuckets = useMemo(() => {
    const filtered = searchedBuckets.filter(
      (b) => matchesFilter(b, filter) && matchesCountry(b, countryFilter),
    );
    return sortBuckets(filtered, sortKey, sortDir);
  }, [searchedBuckets, filter, countryFilter, sortKey, sortDir]);

  return (
    <div className="app">
      <header className="app-header">
        <h1>Gerenciador de Backup MGSIS</h1>
        <div className="app-actions">
          <button onClick={() => load(true)} disabled={loading}>
            {loading ? "Atualizando…" : "Atualizar"}
          </button>
          <button onClick={() => setWsModalOpen(true)} className="secondary">
            Configurar WebService
          </button>
          <button onClick={() => setEmailModalOpen(true)} className="secondary">
            Testar Email
          </button>
        </div>
      </header>

      {error && <p className="error">{error}</p>}
      {clientsError && <p className="error">Clientes do WebService: {clientsError}</p>}

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
          {countryOptions.map((opt) => (
            <button
              key={`country-${opt.key}`}
              type="button"
              className={`filter-chip${countryFilter === opt.key ? " active" : ""}`}
              onClick={() => setCountryFilter(opt.key)}
            >
              {opt.label} ({countryCounts[opt.key]})
            </button>
          ))}
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

      <EmailTestModal isOpen={emailModalOpen} onClose={() => setEmailModalOpen(false)} />

      {wsModalOpen && <WsConfigModal onClose={() => setWsModalOpen(false)} />}
    </div>
  );
}
