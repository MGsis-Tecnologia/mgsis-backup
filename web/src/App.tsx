import { useEffect, useState } from "react";
import { fetchBuckets } from "./api.js";
import type { BucketSummary } from "./types.js";
import { BucketCard } from "./components/BucketCard.js";
import { FileListModal } from "./components/FileListModal.js";

export default function App() {
  const [buckets, setBuckets] = useState<BucketSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [selectedBucket, setSelectedBucket] = useState<BucketSummary | null>(null);

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
  }, []);

  return (
    <div className="app">
      <header className="app-header">
        <h1>Gerenciador de Backup MGSIS</h1>
        <button onClick={() => load(true)} disabled={loading}>
          {loading ? "Atualizando…" : "Atualizar"}
        </button>
      </header>

      {error && <p className="error">{error}</p>}

      {!error && buckets.length === 0 && !loading && <p>Nenhum bucket encontrado.</p>}

      <div className="bucket-grid">
        {buckets.map((bucket) => (
          <BucketCard key={bucket.bucketId} bucket={bucket} onClick={() => setSelectedBucket(bucket)} />
        ))}
      </div>

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
