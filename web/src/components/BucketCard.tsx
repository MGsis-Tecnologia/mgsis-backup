import type { BucketSummary } from "../types.js";
import { formatBytes, formatDate } from "../format.js";

const statusLabel: Record<BucketSummary["status"], string> = {
  ok: "Em dia",
  warning: "Atenção",
  critical: "Crítico",
  empty: "Sem backups",
};

export function BucketCard({ bucket, onClick }: { bucket: BucketSummary; onClick: () => void }) {
  return (
    <button type="button" className={`bucket-card status-${bucket.status}`} onClick={onClick}>
      <div className="bucket-card-header">
        <h2>{bucket.bucketName}</h2>
        <span className={`status-badge status-${bucket.status}`}>{statusLabel[bucket.status]}</span>
      </div>
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
      </dl>
    </button>
  );
}
