import type {
  BucketFile,
  BucketListing,
  ConnectionTestResult,
  PgTargetInput,
  RestoreDefaults,
  RestoreJob,
  WsConfigInfo,
  WsTestResult,
} from "./types.js";

export async function fetchWsConfig(): Promise<WsConfigInfo> {
  const res = await fetch("/api/ws-config");
  if (!res.ok) throw new Error(`Falha ao carregar a configuração (${res.status})`);
  return res.json();
}

export async function saveWsConfig(input: { url: string; token: string; cnpj: string }): Promise<void> {
  const res = await fetch("/api/ws-config", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || `Falha ao salvar (${res.status})`);
}

export async function testWsConnection(): Promise<WsTestResult> {
  const res = await fetch("/api/ws-config/test", { method: "POST" });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || `Falha ao testar (${res.status})`);
  return data;
}

export async function fetchBuckets(refresh = false): Promise<BucketListing> {
  const res = await fetch(`/api/buckets${refresh ? "?refresh=true" : ""}`);
  if (!res.ok) {
    throw new Error(`Failed to fetch buckets: ${res.status}`);
  }
  return res.json();
}

export async function fetchBucketFiles(bucketId: string, refresh = false): Promise<BucketFile[]> {
  const res = await fetch(`/api/buckets/${encodeURIComponent(bucketId)}/files${refresh ? "?refresh=true" : ""}`);
  if (!res.ok) {
    throw new Error(`Failed to fetch files: ${res.status}`);
  }
  const data = await res.json();
  return data.files;
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

export async function sendTestEmail(toEmail: string, subject: string, htmlContent: string): Promise<{ success: boolean; messageId: string }> {
  const res = await fetch("/api/email/test", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ toEmail, subject, htmlContent }),
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || `Falha ao enviar email (${res.status})`);
  return data;
}
