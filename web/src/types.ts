export type BucketStatus = "ok" | "warning" | "critical" | "empty" | "missing";

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
