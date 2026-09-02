import type { BucketFile, BucketSummary, ClientStore } from "./types.js";

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
