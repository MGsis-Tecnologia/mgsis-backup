import { promises as fs } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const currentDir = path.dirname(fileURLToPath(import.meta.url));
const DATA_DIR = path.resolve(currentDir, "../../data");
const CONFIG_FILE = path.join(DATA_DIR, "ws-config.json");

export interface WsConfig {
  url: string;
  token: string;
  cnpj: string;
}

export async function readWsConfig(): Promise<WsConfig | null> {
  try {
    const raw = await fs.readFile(CONFIG_FILE, "utf8");
    const parsed = JSON.parse(raw) as Partial<WsConfig>;
    if (!parsed.url || !parsed.token || !parsed.cnpj) return null;
    return { url: parsed.url, token: parsed.token, cnpj: parsed.cnpj };
  } catch {
    return null;
  }
}

export async function writeWsConfig(input: { url: string; token?: string; cnpj: string }): Promise<WsConfig> {
  const current = await readWsConfig();
  const token = input.token?.trim() ? input.token.trim() : current?.token ?? "";
  if (!token) throw new Error("Informe o token do WebService");

  const config: WsConfig = { url: input.url, token, cnpj: input.cnpj };
  await fs.mkdir(DATA_DIR, { recursive: true });
  await fs.writeFile(CONFIG_FILE, JSON.stringify(config, null, 2), "utf8");
  return config;
}
