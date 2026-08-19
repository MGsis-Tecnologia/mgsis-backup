export type BucketStatus = "ok" | "warning" | "critical" | "empty";

export interface BucketSummary {
  bucketId: string;
  bucketName: string;
  bucketType: string;
  fileCount: number;
  totalSizeBytes: number;
  lastBackupAt: string | null;
  daysSinceLastBackup: number | null;
  status: BucketStatus;
}

export interface BucketFile {
  fileName: string;
  sizeBytes: number;
  uploadedAt: string;
}
