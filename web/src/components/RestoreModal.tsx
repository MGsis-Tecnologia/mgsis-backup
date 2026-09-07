import { useEffect, useRef, useState } from "react";
import {
  DatabaseExistsError,
  fetchRestoreDefaults,
  fetchRestoreJob,
  startRestore,
  testConnection,
} from "../api.js";
import type { BucketFile, ConnectionTestResult, RestoreJob } from "../types.js";
import { formatBytes } from "../format.js";

const phaseLabel: Record<RestoreJob["phase"], string> = {
  preparing: "Criando o banco…",
  downloading: "Baixando o backup do B2…",
  restoring: "Restaurando no PostgreSQL…",
  done: "Restauração concluída",
  error: "Falha na restauração",
};

export function RestoreModal({
  bucketId,
  bucketName,
  file,
  onClose,
}: {
  bucketId: string;
  bucketName: string;
  file: BucketFile;
  onClose: () => void;
}) {
  const [host, setHost] = useState("localhost");
  const [port, setPort] = useState("5432");
  // The database is named after the bucket (the client's RUC) by default.
  const [database, setDatabase] = useState(bucketName);
  const [user, setUser] = useState("postgres");
  const [password, setPassword] = useState("");

  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<ConnectionTestResult | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [overwriteAsked, setOverwriteAsked] = useState(false);
  const [job, setJob] = useState<RestoreJob | null>(null);
  const pollRef = useRef<number | null>(null);

  useEffect(() => {
    fetchRestoreDefaults()
      .then((d) => {
        setHost(d.host);
        setPort(String(d.port));
        setUser(d.user);
      })
      .catch(() => {});
  }, []);

  useEffect(() => {
    return () => {
      if (pollRef.current) window.clearInterval(pollRef.current);
    };
  }, []);

  const target = { host, port: Number(port), database, user, password };
  const running = job !== null && job.phase !== "done" && job.phase !== "error";

  async function handleTest() {
    setTesting(true);
    setTestResult(null);
    setFormError(null);
    try {
      setTestResult(await testConnection(target));
    } catch (err) {
      setFormError(err instanceof Error ? err.message : "Erro ao testar a conexão");
    } finally {
      setTesting(false);
    }
  }

  function pollJob(jobId: string) {
    pollRef.current = window.setInterval(async () => {
      try {
        const updated = await fetchRestoreJob(jobId);
        setJob(updated);
        if (updated.phase === "done" || updated.phase === "error") {
          if (pollRef.current) window.clearInterval(pollRef.current);
          pollRef.current = null;
        }
      } catch {
        // a transient poll failure is not fatal; the next tick retries
      }
    }, 1500);
  }

  async function handleRestore(dropExisting: boolean) {
    setSubmitting(true);
    setFormError(null);
    try {
      const started = await startRestore({
        bucketId,
        fileName: file.fileName,
        sizeBytes: file.sizeBytes,
        target,
        dropExisting,
      });
      setJob(started);
      setOverwriteAsked(false);
      pollJob(started.id);
    } catch (err) {
      if (err instanceof DatabaseExistsError) {
        setOverwriteAsked(true);
      } else {
        setFormError(err instanceof Error ? err.message : "Erro ao iniciar a restauração");
      }
    } finally {
      setSubmitting(false);
    }
  }

  const progressPct =
    job && job.totalBytes > 0 ? Math.min(100, Math.round((job.downloadedBytes / job.totalBytes) * 100)) : 0;

  return (
    <div className="modal-overlay" onClick={running ? undefined : onClose}>
      <div className="modal-panel restore-panel" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <h2>
            <span className="restore-icon" aria-hidden="true">
              ▤
            </span>{" "}
            Restaurar Backup
          </h2>
          <button
            type="button"
            className="modal-close"
            onClick={onClose}
            disabled={running}
            aria-label="Fechar"
          >
            ×
          </button>
        </div>

        <p className="restore-subtitle">Informe os dados de conexão do banco PostgreSQL de destino.</p>

        <div className="restore-file">
          Arquivo: <strong>{file.fileName.split("/").pop()}</strong> · {formatBytes(file.sizeBytes)}
        </div>

        {!job && (
          <>
            <div className="form-row">
              <label className="form-field form-field-grow">
                <span>Host</span>
                <input value={host} onChange={(e) => setHost(e.target.value)} placeholder="localhost" />
              </label>
              <label className="form-field form-field-port">
                <span>Porta</span>
                <input value={port} onChange={(e) => setPort(e.target.value)} inputMode="numeric" />
              </label>
            </div>

            <label className="form-field">
              <span>
                Database <em className="req">*</em>
              </span>
              <input value={database} onChange={(e) => setDatabase(e.target.value)} />
            </label>

            <label className="form-field">
              <span>
                Usuário <em className="req">*</em>
              </span>
              <input value={user} onChange={(e) => setUser(e.target.value)} />
            </label>

            <label className="form-field">
              <span>Senha</span>
              <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
            </label>

            <button type="button" className="btn-test" onClick={handleTest} disabled={testing}>
              {testing ? "Testando…" : "▤  Testar Conexão"}
            </button>

            {testResult?.ok && (
              <p className="test-ok">
                Conexão OK · PostgreSQL {testResult.serverVersion}
                {testResult.databaseExists
                  ? ` · o banco "${database}" já existe`
                  : ` · o banco "${database}" será criado`}
              </p>
            )}
            {testResult && !testResult.ok && <p className="error">{testResult.error}</p>}
            {formError && <p className="error">{formError}</p>}

            {overwriteAsked && (
              <div className="overwrite-warning">
                <strong>O banco "{database}" já existe.</strong>
                <p>
                  Para restaurar, ele precisa ser apagado e recriado. Todos os dados atuais desse banco serão
                  perdidos.
                </p>
                <div className="overwrite-actions">
                  <button type="button" onClick={() => setOverwriteAsked(false)}>
                    Cancelar
                  </button>
                  <button type="button" className="btn-danger" onClick={() => handleRestore(true)}>
                    Apagar e restaurar
                  </button>
                </div>
              </div>
            )}

            <div className="modal-footer">
              <button type="button" onClick={onClose}>
                Cancelar
              </button>
              <button
                type="button"
                className="btn-primary"
                onClick={() => handleRestore(false)}
                disabled={submitting || !database.trim() || !user.trim()}
              >
                {submitting ? "Iniciando…" : "✓  Restaurar"}
              </button>
            </div>
          </>
        )}

        {job && (
          <div className="restore-progress">
            <p className={`phase phase-${job.phase}`}>{phaseLabel[job.phase]}</p>

            {(job.phase === "downloading" || job.phase === "restoring") && (
              <>
                <div className="progress-bar">
                  <div className="progress-fill" style={{ width: `${progressPct}%` }} />
                </div>
                <p className="muted-line">
                  {formatBytes(job.downloadedBytes)}
                  {job.totalBytes > 0 && ` de ${formatBytes(job.totalBytes)} (${progressPct}%)`}
                  {job.phase === "restoring" && " transferidos · o psql ainda está aplicando o dump"}
                </p>
              </>
            )}

            {job.phase === "done" && (
              <p className="test-ok">
                Banco "{job.database}" restaurado com sucesso
                {job.droppedDatabase && " (o banco anterior foi apagado)"}.
              </p>
            )}

            {job.phase === "error" && <p className="error">{job.error}</p>}

            {job.sqlErrorCount > 0 && (
              <details className="sql-errors">
                <summary>
                  {job.sqlErrorCount} aviso(s) do psql
                  {job.phase === "done" && " — a restauração terminou mesmo assim"}
                </summary>
                <pre>{job.sqlErrors.join("\n")}</pre>
              </details>
            )}

            <div className="modal-footer">
              <button type="button" onClick={onClose} disabled={running}>
                {running ? "Restaurando…" : "Fechar"}
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
