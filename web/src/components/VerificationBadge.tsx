import type { VerificationInfo } from "../types.js";
import { formatDate } from "../format.js";

const labels: Record<"ok" | "falha" | "none", string> = {
  ok: "Verificado",
  falha: "Falha",
  none: "Não verificado",
};

export function VerificationBadge({ verification }: { verification: VerificationInfo }) {
  const kind = verification.status === "ok" ? "ok" : verification.status === "falha" ? "falha" : "none";
  return (
    <div className="verif-cell" title={verification.detail ?? undefined}>
      <span className={`verif-badge verif-${kind}`}>{labels[kind]}</span>
      {verification.checkedAt && <span className="verif-date">{formatDate(verification.checkedAt)}</span>}
    </div>
  );
}
