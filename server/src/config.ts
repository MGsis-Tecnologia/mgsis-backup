import "dotenv/config";
import { existsSync } from "node:fs";

// PostgreSQL on Windows is not on PATH by default, so fall back to the usual install location.
function resolvePsqlPath(): string {
  const fromEnv = process.env.PSQL_PATH;
  if (fromEnv) return fromEnv;
  for (const major of ["17", "16", "15", "14", "13", "12"]) {
    const candidate = `C:\\Program Files\\PostgreSQL\\${major}\\bin\\psql.exe`;
    if (existsSync(candidate)) return candidate;
  }
  return "psql";
}

function required(name: string): string {
  const value = process.env[name];
  if (!value) {
    throw new Error(`Missing required environment variable: ${name}`);
  }
  return value;
}

export const config = {
  port: Number(process.env.PORT ?? 4000),
  b2: {
    applicationKeyId: required("B2_APPLICATION_KEY_ID"),
    applicationKey: required("B2_APPLICATION_KEY"),
  },
  kpi: {
    warningAfterDays: Number(process.env.KPI_WARNING_DAYS ?? 2),
    criticalAfterDays: Number(process.env.KPI_CRITICAL_DAYS ?? 7),
  },
  cacheTtlMs: Number(process.env.CACHE_TTL_MS ?? 5 * 60 * 1000),
  psqlPath: resolvePsqlPath(),
  restoreDefaults: {
    host: process.env.RESTORE_DEFAULT_HOST ?? "localhost",
    port: Number(process.env.RESTORE_DEFAULT_PORT ?? 5432),
    user: process.env.RESTORE_DEFAULT_USER ?? "postgres",
  },
};
