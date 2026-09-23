import { getAuthorizedB2 } from "./b2Client.js";
import { getClients } from "./clientStore.js";
import { config } from "../config.js";

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

const NO_VERIFICATION: VerificationInfo = { status: null, checkedAt: null, detail: null, fileName: null };

export interface BucketFile {
  fileName: string;
  sizeBytes: number;
  uploadedAt: string;
}

interface CacheEntry {
  fetchedAt: number;
  files: BucketFile[];
}

interface B2BucketRecord {
  bucketId: string;
  bucketName: string;
  bucketType: string;
}

interface B2FileRecord {
  fileName: string;
  contentLength: number;
  uploadTimestamp: number;
  action: string;
}

const cache = new Map<string, CacheEntry>();
const bucketNameCache = new Map<string, string>();

async function resolveBucketName(bucketId: string): Promise<string> {
  const cached = bucketNameCache.get(bucketId);
  if (cached) return cached;

  const b2 = await getAuthorizedB2();
  const { data } = await b2.listBuckets();
  const buckets = data.buckets as B2BucketRecord[];
  for (const bucket of buckets) bucketNameCache.set(bucket.bucketId, bucket.bucketName);

  const name = bucketNameCache.get(bucketId);
  if (!name) throw new Error(`Bucket not found: ${bucketId}`);
  return name;
}

async function listAllFiles(bucketId: string): Promise<B2FileRecord[]> {
  const b2 = await getAuthorizedB2();
  const files: B2FileRecord[] = [];
  let startFileName = "";

  for (;;) {
    const { data } = await b2.listFileNames({
      bucketId,
      startFileName,
      maxFileCount: 10000,
      delimiter: "",
      prefix: "",
    });
    files.push(...(data.files as B2FileRecord[]));
    const next = data.nextFileName as string | null;
    if (!next) break;
    startFileName = next;
  }

  return files;
}

function isFolderPlaceholder(fileName: string): boolean {
  // Empty-folder marker created by B2-compatible clients (e.g. Cyberduck), not a real backup file
  return fileName.endsWith("/.bzEmpty");
}

async function getCachedFiles(bucketId: string, forceRefresh: boolean): Promise<BucketFile[]> {
  const cached = cache.get(bucketId);
  if (!forceRefresh && cached && Date.now() - cached.fetchedAt < config.cacheTtlMs) {
    return cached.files;
  }

  const files = (await listAllFiles(bucketId))
    .filter((f) => f.action === "upload" && !isFolderPlaceholder(f.fileName))
    .map((f) => ({
      fileName: f.fileName,
      sizeBytes: f.contentLength,
      uploadedAt: new Date(f.uploadTimestamp).toISOString(),
    }));

  cache.set(bucketId, { fetchedAt: Date.now(), files });
  return files;
}

const verificationContentCache = new Map<string, string>();

function isVerificationFile(fileName: string): boolean {
  const lower = fileName.toLowerCase();
  if (!lower.endsWith(".txt")) return false;
  const slash = lower.lastIndexOf("/");
  const dir = slash === -1 ? "" : lower.slice(0, slash);
  return (dir.split("/").pop() ?? "") === "verificacao";
}

async function downloadTextFile(bucketName: string, fileName: string): Promise<string> {
  const cached = verificationContentCache.get(fileName);
  if (cached !== undefined) return cached;

  const b2 = await getAuthorizedB2();
  const { data } = await b2.downloadFileByName({ bucketName, fileName, responseType: "text" });
  const text = typeof data === "string" ? data : String(data);
  verificationContentCache.set(fileName, text);
  return text;
}

function parseVerificationText(text: string): Record<string, string> {
  const fields: Record<string, string> = {};
  for (const line of text.split(/\r?\n/)) {
    const idx = line.indexOf(":");
    if (idx === -1) continue;
    const key = line.slice(0, idx).trim().toLowerCase();
    const value = line.slice(idx + 1).trim();
    if (key) fields[key] = value;
  }
  return fields;
}

async function computeVerification(bucketName: string, files: BucketFile[]): Promise<VerificationInfo> {
  const latest = files
    .filter((f) => isVerificationFile(f.fileName))
    .sort((a, b) => Date.parse(b.uploadedAt) - Date.parse(a.uploadedAt))[0];

  if (!latest) return NO_VERIFICATION;

  try {
    const fields = parseVerificationText(await downloadTextFile(bucketName, latest.fileName));
    const statusRaw = (fields.status ?? "").trim().toLowerCase();
    const status: VerificationStatus = statusRaw === "ok" ? "ok" : statusRaw ? "falha" : null;
    const parsedDate = fields.data ? Date.parse(fields.data) : NaN;
    const checkedAt = Number.isNaN(parsedDate) ? latest.uploadedAt : new Date(parsedDate).toISOString();
    return { status, checkedAt, detail: fields.detalhe ?? fields.detail ?? null, fileName: latest.fileName };
  } catch (err) {
    console.error(
      `Failed to read verification file "${latest.fileName}" for bucket ${bucketName}:`,
      err instanceof Error ? err.message : err,
    );
    return { status: null, checkedAt: latest.uploadedAt, detail: null, fileName: latest.fileName };
  }
}

function computeStatus(daysSinceLastBackup: number | null): BucketStatus {
  if (daysSinceLastBackup === null) return "empty";
  if (daysSinceLastBackup >= config.kpi.criticalAfterDays) return "critical";
  if (daysSinceLastBackup >= config.kpi.warningAfterDays) return "warning";
  return "ok";
}

function summarize(bucketId: string, bucketName: string, bucketType: string, files: BucketFile[]): BucketSummary {
  const fileCount = files.length;
  const totalSizeBytes = files.reduce((sum, f) => sum + f.sizeBytes, 0);
  const latestTimestamp = files.reduce<number | null>((latest, f) => {
    const t = Date.parse(f.uploadedAt);
    return latest === null || t > latest ? t : latest;
  }, null);

  const lastBackupAt = latestTimestamp !== null ? new Date(latestTimestamp).toISOString() : null;
  const daysSinceLastBackup =
    latestTimestamp !== null ? Math.floor((Date.now() - latestTimestamp) / (1000 * 60 * 60 * 24)) : null;

  return {
    bucketId,
    bucketName,
    bucketType,
    fileCount,
    totalSizeBytes,
    lastBackupAt,
    daysSinceLastBackup,
    status: computeStatus(daysSinceLastBackup),
    clientName: null,
    verification: NO_VERIFICATION,
  };
}

function sortByBucketName(a: BucketSummary, b: BucketSummary): number {
  return a.bucketName.localeCompare(b.bucketName, undefined, { numeric: true, sensitivity: "base" });
}

export async function listBucketsWithStats(forceRefresh = false): Promise<BucketSummary[]> {
  const b2 = await getAuthorizedB2();
  const { data } = await b2.listBuckets();
  const buckets = data.buckets as B2BucketRecord[];
  for (const bucket of buckets) bucketNameCache.set(bucket.bucketId, bucket.bucketName);

  const { clients } = await getClients();
  const nameByRuc = new Map(clients.map((c) => [c.ruc.trim(), c.nome]));
  const existingBucketNames = new Set(buckets.map((b) => b.bucketName.trim()));

  const summaries = await Promise.all(
    buckets.map(async (bucket) => {
      const files = await getCachedFiles(bucket.bucketId, forceRefresh);
      const summary = summarize(bucket.bucketId, bucket.bucketName, bucket.bucketType, files);
      summary.clientName = nameByRuc.get(bucket.bucketName.trim()) ?? null;
      summary.verification = await computeVerification(bucket.bucketName, files);
      return summary;
    }),
  );

  // RUCs listed in the imported spreadsheet but with no bucket yet on Backblaze:
  // these are highlighted as "não fazendo backup".
  const missing: BucketSummary[] = clients
    .filter((c) => c.ruc.trim() && !existingBucketNames.has(c.ruc.trim()))
    .map((c) => ({
      bucketId: `missing:${c.ruc}`,
      bucketName: c.ruc,
      bucketType: "—",
      fileCount: 0,
      totalSizeBytes: 0,
      lastBackupAt: null,
      daysSinceLastBackup: null,
      status: "missing" as const,
      clientName: c.nome || null,
      verification: NO_VERIFICATION,
    }));

  return [...summaries, ...missing].sort(sortByBucketName);
}

export async function listBucketFiles(bucketId: string, forceRefresh = false): Promise<BucketFile[]> {
  const files = await getCachedFiles(bucketId, forceRefresh);
  return [...files].sort((a, b) => Date.parse(b.uploadedAt) - Date.parse(a.uploadedAt));
}

const DOWNLOAD_URL_VALID_SECONDS = 300;

export async function getFileDownloadUrl(bucketId: string, fileName: string): Promise<string> {
  const b2 = await getAuthorizedB2();
  const bucketName = await resolveBucketName(bucketId);

  const { data } = await b2.getDownloadAuthorization({
    bucketId,
    fileNamePrefix: fileName,
    validDurationInSeconds: DOWNLOAD_URL_VALID_SECONDS,
  });

  const encodedFileName = fileName.split("/").map(encodeURIComponent).join("/");
  const downloadUrl = (b2 as unknown as { downloadUrl: string }).downloadUrl;
  return `${downloadUrl}/file/${bucketName}/${encodedFileName}?Authorization=${data.authorizationToken}`;
}
