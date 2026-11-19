import { config } from "../config.js";
import { callMgsisOperation } from "./mgsisSoap.js";
import { readWsConfig } from "./wsConfigStore.js";

const PAGE_SIZE = 100;
const MAX_PAGES = 500;

export type Country = "BR" | "PY";

export interface WsClient {
  codigo: string;
  nome: string;
  ruc: string;
  uf: string;
  cidade: string;
  ultimoAcesso: string | null;
  country: Country | null;
  bucketName: string | null;
}

interface CacheState {
  fetchedAt: number;
  clients: WsClient[];
}

let cache: CacheState | null = null;

// Campos da cliente_lista (17 por linha): posição 8 é um apelido, por isso o RUC fica em 9.
function parseLine(line: string): WsClient {
  const f = line.split("|");
  if (f.length < 17) throw new Error(`Linha de cliente com ${f.length} campos (esperado 17)`);

  const documento = f[4].replace(/\D/g, "");
  const ruc = f[9].trim();
  const ultimo = f[13].trim();

  let country: Country | null = null;
  let bucketName: string | null = null;
  if (documento) {
    country = "BR";
    bucketName = documento;
  } else if (ruc) {
    country = "PY";
    bucketName = ruc;
  }

  return {
    codigo: f[0].trim(),
    nome: f[2].trim(),
    ruc,
    uf: f[11].trim(),
    cidade: f[12].trim(),
    ultimoAcesso: /^\d{8}$/.test(ultimo) ? ultimo : null,
    country,
    bucketName,
  };
}

async function fetchAllLines(url: string, token: string, identificacao: string): Promise<string[]> {
  const lines: string[] = [];
  for (let page = 0; page < MAX_PAGES; page++) {
    const results = await callMgsisOperation(url, "cliente_lista", {
      token,
      identificacao,
      inicio: page * PAGE_SIZE,
      limite: PAGE_SIZE,
    });
    const rows = results
      .join("\n")
      .split(/\r\n|\r|\n/)
      .filter((line) => line.trim() !== "");
    lines.push(...rows);
    if (rows.length < PAGE_SIZE) return lines;
  }
  throw new Error("Lista de clientes maior que o limite de paginação");
}

export async function getWsClients(forceRefresh = false): Promise<WsClient[]> {
  if (!forceRefresh && cache && Date.now() - cache.fetchedAt < config.cacheTtlMs) {
    return cache.clients;
  }

  const stored = await readWsConfig();
  if (!stored) throw new Error("WebService não configurado");

  const lines = await fetchAllLines(stored.url, stored.token, stored.cnpj);
  const clients = lines.map(parseLine);
  cache = { fetchedAt: Date.now(), clients };
  return clients;
}
