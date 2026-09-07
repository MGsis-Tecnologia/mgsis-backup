import { spawn } from "node:child_process";
import { randomUUID } from "node:crypto";
import { createGunzip } from "node:zlib";
import { Readable, Transform } from "node:stream";
import { pipeline } from "node:stream/promises";
import { Client } from "pg";
import * as tar from "tar";
import { config } from "../config.js";
import { getFileDownloadUrl } from "./bucketService.js";

export interface PgTarget {
  host: string;
  port: number;
  database: string;
  user: string;
  password: string;
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
  // SQL errors reported by psql: a PG 11 dump restored into PG 13 always trips a few benign ones
  sqlErrors: string[];
  sqlErrorCount: number;
  error: string | null;
  createdDatabase: boolean;
  droppedDatabase: boolean;
}

const jobs = new Map<string, RestoreJob>();
const JOB_TTL_MS = 60 * 60 * 1000;

function pruneJobs(): void {
  const cutoff = Date.now() - JOB_TTL_MS;
  for (const [id, job] of jobs) {
    if (job.finishedAt && Date.parse(job.finishedAt) < cutoff) jobs.delete(id);
  }
}

export function getJob(id: string): RestoreJob | undefined {
  return jobs.get(id);
}

// Postgres identifiers are quoted, so an all-digit bucket name like 80027879 is a valid database name.
function quoteIdent(name: string): string {
  return `"${name.replace(/"/g, '""')}"`;
}

function maintenanceClient(target: PgTarget): Client {
  return new Client({
    host: target.host,
    port: target.port,
    user: target.user,
    password: target.password,
    database: "postgres",
    connectionTimeoutMillis: 10000,
  });
}

export async function testConnection(
  target: PgTarget,
): Promise<{ serverVersion: string; databaseExists: boolean }> {
  const client = maintenanceClient(target);
  await client.connect();
  try {
    const version = await client.query<{ server_version: string }>("SHOW server_version");
    const exists = await client.query("SELECT 1 FROM pg_database WHERE datname = $1", [target.database]);
    return {
      serverVersion: version.rows[0]?.server_version ?? "desconhecida",
      databaseExists: (exists.rowCount ?? 0) > 0,
    };
  } finally {
    await client.end();
  }
}

export class DatabaseExistsError extends Error {
  code = "database_exists";
  constructor(database: string) {
    super(`O banco "${database}" já existe no servidor de destino.`);
  }
}

// Creates the target database, dropping it first only when the caller explicitly asked to overwrite.
// template0 avoids collation mismatches between the dump's cluster and the destination.
async function prepareDatabase(
  target: PgTarget,
  dropExisting: boolean,
): Promise<{ created: boolean; dropped: boolean }> {
  const client = maintenanceClient(target);
  await client.connect();
  try {
    const exists = await client.query("SELECT 1 FROM pg_database WHERE datname = $1", [target.database]);
    let dropped = false;

    if ((exists.rowCount ?? 0) > 0) {
      if (!dropExisting) throw new DatabaseExistsError(target.database);
      await client.query(
        "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()",
        [target.database],
      );
      await client.query(`DROP DATABASE ${quoteIdent(target.database)}`);
      dropped = true;
    }

    await client.query(`CREATE DATABASE ${quoteIdent(target.database)} TEMPLATE template0 ENCODING 'UTF8'`);
    return { created: true, dropped };
  } finally {
    await client.end();
  }
}

const MAX_REPORTED_SQL_ERRORS = 40;

// Streams the backup straight from B2 into psql: fetch -> gunzip -> tar -> psql stdin.
// The archives hold a single plain-SQL dump, so nothing is written to disk along the way.
async function runRestore(job: RestoreJob, target: PgTarget): Promise<void> {
  const url = await getFileDownloadUrl(job.bucketId, job.fileName);
  const response = await fetch(url);
  if (!response.ok || !response.body) {
    throw new Error(`Falha ao baixar o backup do B2 (${response.status})`);
  }

  const declaredLength = Number(response.headers.get("content-length") ?? 0);
  if (declaredLength > 0) job.totalBytes = declaredLength;
  job.phase = "downloading";

  const psql = spawn(
    config.psqlPath,
    [
      "--host", target.host,
      "--port", String(target.port),
      "--username", target.user,
      "--dbname", target.database,
      "--no-password",
      "--quiet",
      // ON_ERROR_STOP stays off on purpose: a PG 11 dump restored into PG 13 trips on
      // settings removed since (default_with_oids) that must not abort the whole restore.
      "--variable=ON_ERROR_STOP=0",
    ],
    {
      env: { ...process.env, PGPASSWORD: target.password, PGCLIENTENCODING: "UTF8" },
      stdio: ["pipe", "pipe", "pipe"],
    },
  );

  let stderrTail = "";
  psql.stderr.setEncoding("utf8");
  psql.stderr.on("data", (chunk: string) => {
    stderrTail = (stderrTail + chunk).slice(-8000);
    for (const line of chunk.split(/\r?\n/)) {
      if (!/\b(ERROR|ERRO|FATAL)\b/i.test(line)) continue;
      job.sqlErrorCount += 1;
      if (job.sqlErrors.length < MAX_REPORTED_SQL_ERRORS) job.sqlErrors.push(line.trim());
    }
  });
  psql.stdout.resume();

  const psqlExit = new Promise<number>((resolve, reject) => {
    psql.on("error", (err) => reject(new Error(`Não foi possível executar o psql: ${err.message}`)));
    psql.on("close", (code) => resolve(code ?? 1));
  });

  const counter = new Transform({
    transform(chunk, _enc, cb) {
      job.downloadedBytes += chunk.length;
      cb(null, chunk);
    },
  });

  let sqlEntryFound = false;
  const parser = new tar.Parser();
  parser.on("entry", (entry) => {
    // Each archive holds exactly one dump file; anything else is drained so the stream keeps flowing.
    if (sqlEntryFound || entry.type !== "File") {
      entry.resume();
      return;
    }
    sqlEntryFound = true;
    job.phase = "restoring";
    entry.pipe(psql.stdin);
  });

  try {
    await pipeline(Readable.fromWeb(response.body as never), counter, createGunzip(), parser);
  } catch (err) {
    psql.stdin.destroy();
    psql.kill();
    throw err;
  }

  if (!sqlEntryFound) {
    psql.stdin.end();
    psql.kill();
    throw new Error("Nenhum arquivo de dump encontrado dentro do .tar.gz");
  }

  const exitCode = await psqlExit;
  if (exitCode !== 0) {
    const detail = stderrTail.trim().split(/\r?\n/).slice(-3).join(" | ");
    throw new Error(`psql terminou com código ${exitCode}${detail ? `: ${detail}` : ""}`);
  }
}

export function startRestore(params: {
  bucketId: string;
  fileName: string;
  sizeBytes: number;
  target: PgTarget;
  dropExisting: boolean;
}): RestoreJob {
  pruneJobs();

  const job: RestoreJob = {
    id: randomUUID(),
    bucketId: params.bucketId,
    fileName: params.fileName,
    database: params.target.database,
    phase: "preparing",
    downloadedBytes: 0,
    totalBytes: params.sizeBytes,
    startedAt: new Date().toISOString(),
    finishedAt: null,
    sqlErrors: [],
    sqlErrorCount: 0,
    error: null,
    createdDatabase: false,
    droppedDatabase: false,
  };
  jobs.set(job.id, job);

  void (async () => {
    try {
      const { created, dropped } = await prepareDatabase(params.target, params.dropExisting);
      job.createdDatabase = created;
      job.droppedDatabase = dropped;
      await runRestore(job, params.target);
      job.phase = "done";
    } catch (err) {
      job.phase = "error";
      job.error = err instanceof Error ? err.message : String(err);
    } finally {
      job.finishedAt = new Date().toISOString();
    }
  })();

  return job;
}

// Validates the request body before any of it reaches psql or the connection.
export function parseTarget(body: unknown): PgTarget {
  const b = (body ?? {}) as Record<string, unknown>;
  const host = typeof b.host === "string" && b.host.trim() ? b.host.trim() : "localhost";
  const port = Number(b.port ?? 5432);
  const database = typeof b.database === "string" ? b.database.trim() : "";
  const user = typeof b.user === "string" ? b.user.trim() : "";
  const password = typeof b.password === "string" ? b.password : "";

  if (!database) throw new Error("Informe o nome do banco de destino");
  if (!user) throw new Error("Informe o usuário do PostgreSQL");
  if (!Number.isInteger(port) || port < 1 || port > 65535) throw new Error("Porta inválida");

  return { host, port, database, user, password };
}
