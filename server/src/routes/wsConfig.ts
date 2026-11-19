import { Router } from "express";
import { callMgsisOperation } from "../services/mgsisSoap.js";
import { readWsConfig, writeWsConfig } from "../services/wsConfigStore.js";

export const wsConfigRouter = Router();

function onlyDigits(value: string): string {
  return value.replace(/\D/g, "");
}

wsConfigRouter.get("/", async (_req, res) => {
  const stored = await readWsConfig();
  res.json({
    url: stored?.url ?? "",
    cnpj: stored?.cnpj ?? "",
    hasToken: Boolean(stored?.token),
  });
});

wsConfigRouter.put("/", async (req, res) => {
  const url = typeof req.body?.url === "string" ? req.body.url.trim() : "";
  const cnpj = typeof req.body?.cnpj === "string" ? req.body.cnpj.trim() : "";
  const token = typeof req.body?.token === "string" ? req.body.token : undefined;

  if (!/^https?:\/\/\S+$/i.test(url)) {
    res.status(400).json({ error: "Informe a URL do WebService começando com http:// ou https://" });
    return;
  }
  if (onlyDigits(cnpj).length !== 14) {
    res.status(400).json({ error: "Informe um CNPJ válido (14 dígitos)" });
    return;
  }

  try {
    await writeWsConfig({ url, token, cnpj });
    res.json({ ok: true, hasToken: true });
  } catch (err) {
    res.status(400).json({ error: err instanceof Error ? err.message : String(err) });
  }
});

wsConfigRouter.post("/test", async (_req, res) => {
  const stored = await readWsConfig();
  if (!stored) {
    res.status(400).json({ error: "Salve a configuração do WebService antes de testar" });
    return;
  }

  try {
    const result = await callMgsisOperation(stored.url, "cliente_lista", {
      token: stored.token,
      identificacao: stored.cnpj,
      inicio: 0,
      limite: 1,
    });
    if (result.some((line) => /acesso negado/i.test(line))) {
      res.json({ ok: false, error: "Acesso negado pelo WebService: confira o token e o CNPJ" });
      return;
    }
    res.json({ ok: true, result });
  } catch (err) {
    res.status(200).json({ ok: false, error: err instanceof Error ? err.message : String(err) });
  }
});
