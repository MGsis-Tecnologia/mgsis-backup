import type { BucketSummary } from "../types.js";
import { formatBytes, formatDate } from "../format.js";
import { VerificationBadge } from "./VerificationBadge.js";
import { CountryFlag } from "./CountryFlag.js";

const statusLabel: Record<BucketSummary["status"], string> = {
  ok: "Em dia",
  warning: "Atenção",
  critical: "Crítico",
  empty: "Sem backups",
  missing: "Atraso",
};

export function BucketTable({
  buckets,
  onSelect,
}: {
  buckets: BucketSummary[];
  onSelect: (bucket: BucketSummary) => void;
}) {
  return (
    <div className="bucket-table-wrap">
      <table className="bucket-table">
        <thead>
          <tr>
            <th>Status</th>
            <th>País</th>
            <th>Bucket</th>
            <th>Cliente</th>
            <th>Último backup</th>
            <th>Dias sem backup</th>
            <th>Arquivos</th>
            <th>Tamanho total</th>
            <th>Verificação</th>
          </tr>
        </thead>
        <tbody>
          {buckets.map((bucket) => {
            const isMissing = bucket.status === "missing";
            const hasKey = bucket.bucketName.trim() !== "";

            return (
              <tr
                key={bucket.bucketId}
                className={`bucket-row status-${bucket.status}${isMissing ? "" : " clickable"}`}
                onClick={() => !isMissing && onSelect(bucket)}
              >
                <td>
                  <span className={`status-badge status-${bucket.status}`}>{statusLabel[bucket.status]}</span>
                </td>
                <td>
                  <CountryFlag country={bucket.country} />
                </td>
                <td>
                  {isMissing ? (
                    <span className="no-bucket">Sem Bucket</span>
                  ) : (
                    <span className="bucket-row-title">{bucket.bucketName}</span>
                  )}
                </td>
                <td>
                  <div className="bucket-row-title">
                    {bucket.clientName ?? (isMissing ? "" : "Sem cliente no WebService")}
                  </div>
                  {isMissing && (
                    <div className="bucket-row-subtitle">{hasKey ? bucket.bucketName : "sem CNPJ/RUC"}</div>
                  )}
                </td>
                <td>{formatDate(bucket.lastBackupAt)}</td>
                <td>{bucket.daysSinceLastBackup ?? "—"}</td>
                <td>{bucket.fileCount.toLocaleString("pt-BR")}</td>
                <td>{formatBytes(bucket.totalSizeBytes)}</td>
                <td>
                  <VerificationBadge verification={bucket.verification} />
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
