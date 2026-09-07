import type {
  BucketFile,
  BucketSummary,
  ClientStore,
  ConnectionTestResult,
  PgTargetInput,
  RestoreDefaults,
  RestoreJob,
} from "./types.js";

export async function fetchBuckets(refresh = false): Promise<BucketSummary[]> {
  const res = await fetch(`/api/buckets${refresh ? "?refresh=true" : ""}`);
  if (!res.ok) {
    throw new Error(`Failed to fetch buckets: ${res.status}`);
  }
  const data = await res.json();
  return data.buckets;
}

export async function fetchBucketFiles(bucketId: string, refresh = false): Promise<BucketFile[]> {
  const res = await fetch(`/api/buckets/${encodeURIComponent(bucketId)}/files${refresh ? "?refresh=true" : ""}`);
  if (!res.ok) {
    throw new Error(`Failed to fetch files: ${res.status}`);
  }
  const data = await res.json();
  return data.files;
}

export async function fetchClients(): Promise<ClientStore> {
  const res = await fetch("/api/clients");
  if (!res.ok) {
    throw new Error(`Failed to fetch clients: ${res.status}`);
  }
  return res.json();
}

export async function importClients(file: File): Promise<ClientStore> {
  const res = await fetch("/api/clients/import", {
    method: "POST",
    headers: { "Content-Type": "application/octet-stream" },
    body: file,
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(data.error || `Falha ao importar (${res.status})`);
  }
  return data;
}

export async function fetchDownloadUrl(bucketId: string, fileName: string): Promise<string> {
  const res = await fetch(
    `/api/buckets/${encodeURIComponent(bucketId)}/download?fileName=${encodeURIComponent(fileName)}`,
  );
  if (!res.ok) {
    throw new Error(`Failed to create download link: ${res.status}`);
  }
  const data = await res.json();
  return data.url;
}

export async function fetchRestoreDefaults(): Promise<RestoreDefaults> {
  const res = await fetch("/api/restore-defaults");
  if (!res.ok) throw new Error(`Failed to fetch defaults: ${res.status}`);
  return res.json();
}

export async function testConnection(target: PgTargetInput): Promise<ConnectionTestResult> {
  const res = await fetch("/api/restore/test-connection", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(target),
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || `Falha ao testar conexão (${res.status})`);
  return data;
}

export class DatabaseExistsError extends Error {}

export async function startRestore(params: {
  bucketId: string;
  fileName: string;
  sizeBytes: number;
  target: PgTargetInput;
  dropExisting: boolean;
}): Promise<RestoreJob> {
  const res = await fetch("/api/restore", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      ...params.target,
      bucketId: params.bucketId,
      fileName: params.fileName,
      sizeBytes: params.sizeBytes,
      dropExisting: params.dropExisting,
    }),
  });
  const data = await res.json().catch(() => ({}));
  if (res.status === 409 && data.code === "database_exists") {
    throw new DatabaseExistsError(data.error);
  }
  if (!res.ok) throw new Error(data.error || `Falha ao iniciar a restauração (${res.status})`);
  return data.job;
}

export async function fetchRestoreJob(jobId: string): Promise<RestoreJob> {
  const res = await fetch(`/api/restore/${encodeURIComponent(jobId)}`);
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || `Falha ao consultar o job (${res.status})`);
  return data.job;
}
