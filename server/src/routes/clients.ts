import { raw, Router } from "express";
import { getClients, importClientsFromSpreadsheet } from "../services/clientStore.js";

export const clientsRouter = Router();

clientsRouter.get("/", async (_req, res) => {
  res.json(await getClients());
});

clientsRouter.post("/import", raw({ type: () => true, limit: "25mb" }), async (req, res) => {
  const buffer = req.body as Buffer;
  if (!Buffer.isBuffer(buffer) || buffer.length === 0) {
    res.status(400).json({ error: "Arquivo vazio ou não enviado." });
    return;
  }
  try {
    const store = await importClientsFromSpreadsheet(buffer);
    res.json(store);
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    console.error("Failed to import clients:", message);
    res.status(400).json({ error: message });
  }
});
