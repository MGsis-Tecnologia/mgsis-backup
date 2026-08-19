import "dotenv/config";

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
};
