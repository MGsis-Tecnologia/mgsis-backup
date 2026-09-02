import { useEffect, useMemo, useRef, useState } from "react";
import { fetchBuckets, fetchClients, importClients } from "./api.js";
import type { BucketSummary, ClientStore } from "./types.js";
import { BucketCard } from "./components/BucketCard.js";
import { FileListModal } from "./components/FileListModal.js";

type Filter = "todos" | "emDia" | "emAtraso";

const filterOptions: { key: Filter; label: string }[] = [
  { key: "todos", label: "Todos" },
  { key: "emDia", label: "Em dia" },
  { key: "emAtraso", label: "Em atraso" },
];

function matchesFilter(bucket: BucketSummary, filter: Filter): boolean {
  if (filter === "todos") return true;
  if (filter === "emDia") return bucket.status === "ok";
  return bucket.status !== "ok"; // em atraso: warning, critical, empty e não fazendo backup
}

export default function App() {
  const [buckets, setBuckets] = useState<BucketSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [selectedBucket, setSelectedBucket] = useState<BucketSummary | null>(null);
  const [filter, setFilter] = useState<Filter>("todos");

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

  const counts = useMemo(() => {
    const missing = buckets.filter((b) => b.status === "missing").length;
    const emDia = buckets.filter((b) => b.status === "ok").length;
    return { missing, emDia, emAtraso: buckets.length - emDia };
  }, [buckets]);

  const visibleBuckets = buckets.filter((b) => matchesFilter(b, filter));

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
              {opt.key === "todos" && ` (${buckets.length})`}
            </button>
          ))}
          {counts.missing > 0 && (
            <span className="missing-hint">{counts.missing} não fazendo backup</span>
          )}
        </div>
      )}

      <div className="bucket-grid">
        {visibleBuckets.map((bucket) => (
          <BucketCard
            key={bucket.bucketId}
            bucket={bucket}
            onClick={() => bucket.status !== "missing" && setSelectedBucket(bucket)}
          />
        ))}
      </div>

      {buckets.length > 0 && visibleBuckets.length === 0 && (
        <p className="muted-line">Nenhum bucket neste filtro.</p>
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
