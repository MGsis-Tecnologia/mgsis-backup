import { useEffect, useMemo, useState } from "react";
import { fetchBucketFiles, fetchDownloadUrl } from "../api.js";
import type { BucketFile } from "../types.js";
import { formatBytes, formatDate } from "../format.js";
import { RestoreModal } from "./RestoreModal.js";

interface Folder {
  /** Full B2 prefix, e.g. "backups/diario" — "" for files sitting at the bucket root */
  path: string;
  label: string;
  files: BucketFile[];
  totalSizeBytes: number;
  lastUploadedAt: string;
}

const ROOT_LABEL = "(raiz)";

function groupIntoFolders(files: BucketFile[]): Folder[] {
  const byPath = new Map<string, BucketFile[]>();
  for (const file of files) {
    const slash = file.fileName.lastIndexOf("/");
    const path = slash === -1 ? "" : file.fileName.slice(0, slash);
    const bucket = byPath.get(path);
    if (bucket) bucket.push(file);
    else byPath.set(path, [file]);
  }

  return [...byPath.entries()]
    .map(([path, folderFiles]) => ({
      path,
      label: path === "" ? ROOT_LABEL : path.split("/").pop() || path,
      files: folderFiles,
      totalSizeBytes: folderFiles.reduce((sum, f) => sum + f.sizeBytes, 0),
      // files arrive newest-first from the API, so the first entry is the latest upload
      lastUploadedAt: folderFiles[0].uploadedAt,
    }))
    .sort((a, b) => Date.parse(b.lastUploadedAt) - Date.parse(a.lastUploadedAt));
}

/** Strips the folder prefix so the file view shows just the file name. */
function baseName(fileName: string): string {
  return fileName.split("/").pop() || fileName;
}

export function FileListModal({
  bucketId,
  bucketName,
  onClose,
}: {
  bucketId: string;
  bucketName: string;
  onClose: () => void;
}) {
  const [files, setFiles] = useState<BucketFile[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [downloadingFile, setDownloadingFile] = useState<string | null>(null);
  const [downloadError, setDownloadError] = useState<string | null>(null);
  const [restoreFile, setRestoreFile] = useState<BucketFile | null>(null);
  const [openFolder, setOpenFolder] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    setOpenFolder(null);
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

  const folders = useMemo(() => groupIntoFolders(files), [files]);
  const current = openFolder === null ? null : folders.find((f) => f.path === openFolder) ?? null;

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
          <h2>
            {current ? (
              <>
                <button type="button" className="crumb-link" onClick={() => setOpenFolder(null)}>
                  {bucketName}
                </button>
                <span className="crumb-sep"> / </span>
                {current.label}
              </>
            ) : (
              bucketName
            )}
          </h2>
          <button type="button" className="modal-close" onClick={onClose} aria-label="Fechar">
            ×
          </button>
        </div>

        {loading && <p>Carregando arquivos…</p>}
        {error && <p className="error">{error}</p>}
        {downloadError && <p className="error">{downloadError}</p>}
        {!loading && !error && files.length === 0 && (
          <p>Nenhum arquivo de backup encontrado neste bucket.</p>
        )}

        {!loading && !error && !current && folders.length > 0 && (
          <table className="file-table">
            <thead>
              <tr>
                <th>Pasta</th>
                <th>Arquivos</th>
                <th>Tamanho</th>
                <th>Último envio</th>
              </tr>
            </thead>
            <tbody>
              {folders.map((folder) => (
                <tr key={folder.path}>
                  <td>
                    <button
                      type="button"
                      className="folder-link"
                      onClick={() => setOpenFolder(folder.path)}
                      title="Abrir pasta"
                    >
                      <span className="folder-icon" aria-hidden="true">
                        ▸
                      </span>
                      {folder.label}
                    </button>
                  </td>
                  <td>{folder.files.length}</td>
                  <td>{formatBytes(folder.totalSizeBytes)}</td>
                  <td>{formatDate(folder.lastUploadedAt)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}

        {!loading && !error && current && (
          <>
            <button type="button" className="back-link" onClick={() => setOpenFolder(null)}>
              ← Voltar para as pastas
            </button>
            <table className="file-table">
              <thead>
                <tr>
                  <th>Arquivo</th>
                  <th>Tamanho</th>
                  <th>Enviado em</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {current.files.map((file) => (
                  <tr key={file.fileName}>
                    <td>
                      <button
                        type="button"
                        className="file-name-link"
                        onClick={() => handleDownload(file.fileName)}
                        disabled={downloadingFile === file.fileName}
                        title="Baixar arquivo"
                      >
                        {downloadingFile === file.fileName
                          ? "Preparando download…"
                          : baseName(file.fileName)}
                      </button>
                    </td>
                    <td>{formatBytes(file.sizeBytes)}</td>
                    <td>{formatDate(file.uploadedAt)}</td>
                    <td>
                      <button
                        type="button"
                        className="btn-restore-row"
                        onClick={() => setRestoreFile(file)}
                        title="Restaurar este backup em um PostgreSQL"
                      >
                        Restaurar
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </>
        )}
      </div>

      {restoreFile && (
        <RestoreModal
          bucketId={bucketId}
          bucketName={bucketName}
          file={restoreFile}
          onClose={() => setRestoreFile(null)}
        />
      )}
    </div>
  );
}
