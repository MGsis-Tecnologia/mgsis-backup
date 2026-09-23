export type BucketStatus = "ok" | "warning" | "critical" | "empty" | "missing";
export type VerificationStatus = "ok" | "falha" | null;

export interface VerificationInfo {
  status: VerificationStatus;
  checkedAt: string | null;
  detail: string | null;
  fileName: string | null;
}

export interface BucketSummary {
  bucketId: string;
  bucketName: string;
  bucketType: string;
  fileCount: number;
  totalSizeBytes: number;
  lastBackupAt: string | null;
  daysSinceLastBackup: number | null;
  status: BucketStatus;
  clientName: string | null;
  verification: VerificationInfo;
}

export interface BucketFile {
  fileName: string;
  sizeBytes: number;
  uploadedAt: string;
}

export interface ClientEntry {
  nome: string;
  ruc: string;
}

export interface ClientStore {
  updatedAt: string | null;
  clients: ClientEntry[];
}

export type RestorePhase = "preparing" | "downloading" | "restoring" | "done" | "error";

export interface RestoreJob {
  id: string;
  bucketId: string;
  fileName: string;
  database: string;
  phase: RestorePhase;
  downloadedBytes: number;
  totalBytes: number;
  startedAt: string;
  finishedAt: string | null;
  sqlErrors: string[];
  sqlErrorCount: number;
  error: string | null;
  createdDatabase: boolean;
  droppedDatabase: boolean;
}

export interface PgTargetInput {
  host: string;
  port: number;
  database: string;
  user: string;
  password: string;
}

export interface RestoreDefaults {
  host: string;
  port: number;
  user: string;
}

export interface ConnectionTestResult {
  ok: boolean;
  serverVersion?: string;
  databaseExists?: boolean;
  error?: string;
}
