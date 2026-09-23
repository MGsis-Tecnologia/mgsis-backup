import type { BucketSummary } from "../types.js";
import { formatBytes, formatDate } from "../format.js";
import { VerificationBadge } from "./VerificationBadge.js";

const statusLabel: Record<BucketSummary["status"], string> = {
  ok: "Em dia",
  warning: "Atenção",
  critical: "Crítico",
  empty: "Sem backups",
  missing: "Atraso",
};

export function BucketCard({ bucket, onClick }: { bucket: BucketSummary; onClick: () => void }) {
  const isMissing = bucket.status === "missing";
  const title = isMissing ? bucket.clientName ?? bucket.bucketName : bucket.bucketName;
  const subtitle = isMissing
    ? `RUC ${bucket.bucketName} · bucket ainda não criado`
    : bucket.clientName;

  const body = (
    <>
      <div className="bucket-card-header">
        <h2>{title}</h2>
        <span className={`status-badge status-${bucket.status}`}>{statusLabel[bucket.status]}</span>
      </div>
      {subtitle && <p className="bucket-subtitle">{subtitle}</p>}
      <dl className="bucket-stats">
        <div>
          <dt>Último backup</dt>
          <dd>{formatDate(bucket.lastBackupAt)}</dd>
        </div>
        <div>
          <dt>Dias sem backup</dt>
          <dd>{bucket.daysSinceLastBackup ?? "—"}</dd>
        </div>
        <div>
          <dt>Arquivos</dt>
          <dd>{bucket.fileCount.toLocaleString("pt-BR")}</dd>
        </div>
        <div>
          <dt>Tamanho total</dt>
          <dd>{formatBytes(bucket.totalSizeBytes)}</dd>
        </div>
        <div className="bucket-stats-full">
          <dt>Verificação</dt>
          <dd>
            <VerificationBadge verification={bucket.verification} />
          </dd>
        </div>
      </dl>
    </>
  );

  if (isMissing) {
    return <div className="bucket-card status-missing">{body}</div>;
  }

  return (
    <button type="button" className={`bucket-card status-${bucket.status}`} onClick={onClick}>
      {body}
    </button>
  );
}
