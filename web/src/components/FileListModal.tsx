import { useEffect, useState } from "react";
import { fetchBucketFiles, fetchDownloadUrl } from "../api.js";
import type { BucketFile } from "../types.js";
import { formatBytes, formatDate } from "../format.js";

export function FileListModal({ bucketId, bucketName, onClose }: { bucketId: string; bucketName: string; onClose: () => void }) {
  const [files, setFiles] = useState<BucketFile[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [downloadingFile, setDownloadingFile] = useState<string | null>(null);
  const [downloadError, setDownloadError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    fetchBucketFiles(bucketId)
      .then((data) => {
        if (!cancelled) setFiles(data);
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof Error ? err.message : "Erro desconhecido");
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [bucketId]);

  async function handleDownload(fileName: string) {
    // Open the tab synchronously so the browser still treats it as user-initiated
    // (an await before window.open would get it blocked as a popup).
    const tab = window.open("", "_blank");
    setDownloadingFile(fileName);
    setDownloadError(null);
    try {
      const url = await fetchDownloadUrl(bucketId, fileName);
      if (tab) {
        tab.location.href = url;
      } else {
        window.location.href = url;
      }
    } catch (err) {
      tab?.close();
      setDownloadError(err instanceof Error ? err.message : "Erro ao gerar link de download");
    } finally {
      setDownloadingFile(null);
    }
  }

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal-panel" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <h2>{bucketName}</h2>
          <button type="button" className="modal-close" onClick={onClose} aria-label="Fechar">
            ×
          </button>
        </div>

        {loading && <p>Carregando arquivos…</p>}
        {error && <p className="error">{error}</p>}
        {downloadError && <p className="error">{downloadError}</p>}
        {!loading && !error && files.length === 0 && <p>Nenhum arquivo de backup encontrado neste bucket.</p>}

        {!loading && !error && files.length > 0 && (
          <table className="file-table">
            <thead>
              <tr>
                <th>Arquivo</th>
                <th>Tamanho</th>
                <th>Enviado em</th>
              </tr>
            </thead>
            <tbody>
              {files.map((file) => (
                <tr key={file.fileName}>
                  <td>
                    <button
                      type="button"
                      className="file-name-link"
                      onClick={() => handleDownload(file.fileName)}
                      disabled={downloadingFile === file.fileName}
                      title="Baixar arquivo"
                    >
                      {downloadingFile === file.fileName ? "Preparando download…" : file.fileName}
                    </button>
                  </td>
                  <td>{formatBytes(file.sizeBytes)}</td>
                  <td>{formatDate(file.uploadedAt)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
