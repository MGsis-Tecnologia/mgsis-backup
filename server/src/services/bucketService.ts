import { getAuthorizedB2 } from "./b2Client.js";
import { getWsClients, type Country } from "./mgsisClients.js";
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
  country: Country | null;
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

function isDiarioFile(fileName: string): boolean {
  const lower = fileName.toLowerCase();
  const slash = lower.lastIndexOf("/");
  const dir = slash === -1 ? "" : lower.slice(0, slash);
  return (dir.split("/").pop() ?? "") === "diario";
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

const DAY_MS = 1000 * 60 * 60 * 24;

// Não há backup diário aos domingos: o tempo que cai em domingo (horário local do
// servidor) não conta como atraso.
function msExcludingSundays(start: number, end: number): number {
  let total = 0;
  let cursor = start;
  while (cursor < end) {
    const nextMidnight = new Date(cursor);
    nextMidnight.setHours(24, 0, 0, 0);
    const segmentEnd = Math.min(nextMidnight.getTime(), end);
    if (new Date(cursor).getDay() !== 0) total += segmentEnd - cursor;
    cursor = segmentEnd;
  }
  return total;
}

function summarize(bucketId: string, bucketName: string, bucketType: string, files: BucketFile[]): BucketSummary {
  const fileCount = files.length;
  const totalSizeBytes = files.reduce((sum, f) => sum + f.sizeBytes, 0);
  // "Último backup" / "Dias sem backup" refletem apenas a pasta diario/ — outras pastas
  // (fisico, fotos, logs, verificacao) têm cadência diferente e não podem mascarar um
  // backup diário perdido.
  const latestTimestamp = files.reduce<number | null>((latest, f) => {
    if (!isDiarioFile(f.fileName)) return latest;
    const t = Date.parse(f.uploadedAt);
    return latest === null || t > latest ? t : latest;
  }, null);

  const lastBackupAt = latestTimestamp !== null ? new Date(latestTimestamp).toISOString() : null;
  const daysSinceLastBackup =
    latestTimestamp !== null ? Math.floor(msExcludingSundays(latestTimestamp, Date.now()) / DAY_MS) : null;

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
    country: null,
    verification: NO_VERIFICATION,
  };
}

function sortByBucketName(a: BucketSummary, b: BucketSummary): number {
  return a.bucketName.localeCompare(b.bucketName, undefined, { numeric: true, sensitivity: "base" });
}

export interface BucketListing {
  buckets: BucketSummary[];
  clientsError: string | null;
}

export async function listBucketsWithStats(forceRefresh = false): Promise<BucketListing> {
  const b2 = await getAuthorizedB2();
  const { data } = await b2.listBuckets();
  const buckets = data.buckets as B2BucketRecord[];
  for (const bucket of buckets) bucketNameCache.set(bucket.bucketId, bucket.bucketName);

  let clients: Awaited<ReturnType<typeof getWsClients>> = [];
  let clientsError: string | null = null;
  try {
    clients = await getWsClients(forceRefresh);
  } catch (err) {
    clientsError = err instanceof Error ? err.message : String(err);
    console.error("Failed to load clients from WebService:", clientsError);
  }

  const clientByBucket = new Map(
    clients.filter((c) => c.bucketName).map((c) => [c.bucketName as string, c]),
  );
  const existingBucketNames = new Set(buckets.map((b) => b.bucketName.trim()));

  const summaries = await Promise.all(
    buckets.map(async (bucket) => {
      const files = await getCachedFiles(bucket.bucketId, forceRefresh);
      const summary = summarize(bucket.bucketId, bucket.bucketName, bucket.bucketType, files);
      const client = clientByBucket.get(bucket.bucketName.trim());
      summary.clientName = client?.nome || null;
      summary.country = client?.country ?? null;
      summary.verification = await computeVerification(bucket.bucketName, files);
      return summary;
    }),
  );

  // Clientes do WebService sem bucket no Backblaze (inclusive sem CNPJ/RUC): aparecem como "não fazendo backup".
  const missing: BucketSummary[] = clients
    .filter((c) => !c.bucketName || !existingBucketNames.has(c.bucketName))
    .map((c) => ({
      bucketId: `missing:${c.codigo}`,
      bucketName: c.bucketName ?? "",
      bucketType: "—",
      fileCount: 0,
      totalSizeBytes: 0,
      lastBackupAt: null,
      daysSinceLastBackup: null,
      status: "missing" as const,
      clientName: c.nome || null,
      country: c.country,
      verification: NO_VERIFICATION,
    }));

  return { buckets: [...summaries, ...missing].sort(sortByBucketName), clientsError };
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
