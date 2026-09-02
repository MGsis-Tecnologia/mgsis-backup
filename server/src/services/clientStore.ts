import { promises as fs } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import * as XLSX from "xlsx";

const currentDir = path.dirname(fileURLToPath(import.meta.url));
const DATA_DIR = path.resolve(currentDir, "../../data");
const CLIENTS_FILE = path.join(DATA_DIR, "clients.json");

export interface ClientEntry {
  nome: string;
  ruc: string;
}

export interface ClientStore {
  updatedAt: string | null;
  clients: ClientEntry[];
}

let memo: ClientStore | null = null;

export async function getClients(): Promise<ClientStore> {
  if (memo) return memo;
  try {
    const raw = await fs.readFile(CLIENTS_FILE, "utf8");
    const parsed = JSON.parse(raw) as Partial<ClientStore>;
    memo = {
      updatedAt: parsed.updatedAt ?? null,
      clients: Array.isArray(parsed.clients) ? parsed.clients : [],
    };
  } catch {
    memo = { updatedAt: null, clients: [] };
  }
  return memo;
}

function normalizeHeader(key: string): string {
  return key.trim().toLowerCase().replace(/\s+/g, "_");
}

export async function importClientsFromSpreadsheet(buffer: Buffer): Promise<ClientStore> {
  let workbook: XLSX.WorkBook;
  try {
    workbook = XLSX.read(buffer, { type: "buffer" });
  } catch {
    throw new Error("Não foi possível ler a planilha. Envie um arquivo .xlsx, .xls ou .csv válido.");
  }

  const sheetName = workbook.SheetNames[0];
  if (!sheetName) throw new Error("A planilha não contém nenhuma aba.");

  const rows = XLSX.utils.sheet_to_json<Record<string, unknown>>(workbook.Sheets[sheetName], { defval: "" });
  if (rows.length === 0) throw new Error("A planilha está vazia.");

  const headerMap = new Map<string, string>();
  for (const key of Object.keys(rows[0])) headerMap.set(normalizeHeader(key), key);
  const nomeKey = headerMap.get("pessoa_nome");
  const rucKey = headerMap.get("pessoa_ruc");
  if (!nomeKey || !rucKey) {
    throw new Error(
      `A planilha precisa ter as colunas "pessoa_nome" e "pessoa_ruc". Colunas encontradas: ${Object.keys(rows[0]).join(", ") || "(nenhuma)"}`,
    );
  }

  const byRuc = new Map<string, ClientEntry>();
  for (const row of rows) {
    const ruc = String(row[rucKey] ?? "").trim();
    const nome = String(row[nomeKey] ?? "").trim();
    if (!ruc) continue;
    byRuc.set(ruc, { nome, ruc });
  }

  const clients = [...byRuc.values()].sort((a, b) =>
    a.ruc.localeCompare(b.ruc, undefined, { numeric: true }),
  );
  if (clients.length === 0) {
    throw new Error("Nenhuma linha com a coluna pessoa_ruc preenchida foi encontrada.");
  }

  const store: ClientStore = { updatedAt: new Date().toISOString(), clients };
  await fs.mkdir(DATA_DIR, { recursive: true });
  await fs.writeFile(CLIENTS_FILE, JSON.stringify(store, null, 2), "utf8");
  memo = store;
  return store;
}
