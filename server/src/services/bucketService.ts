import { getAuthorizedB2 } from "./b2Client.js";
import { config } from "../config.js";

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
  };
}

export async function listBucketsWithStats(forceRefresh = false): Promise<BucketSummary[]> {
  const b2 = await getAuthorizedB2();
  const { data } = await b2.listBuckets();
  const buckets = data.buckets as B2BucketRecord[];
  for (const bucket of buckets) bucketNameCache.set(bucket.bucketId, bucket.bucketName);

  return Promise.all(
    buckets.map(async (bucket) => {
      const files = await getCachedFiles(bucket.bucketId, forceRefresh);
      return summarize(bucket.bucketId, bucket.bucketName, bucket.bucketType, files);
    }),
  );
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
