import { Router } from "express";
import {
  DatabaseExistsError,
  getJob,
  parseTarget,
  startRestore,
  testConnection,
} from "../services/restoreService.js";

export const restoreRouter = Router();

restoreRouter.post("/test-connection", async (req, res) => {
  let target;
  try {
    target = parseTarget(req.body);
  } catch (err) {
    res.status(400).json({ error: err instanceof Error ? err.message : String(err) });
    return;
  }

  try {
    const result = await testConnection(target);
    res.json({ ok: true, ...result });
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    res.status(200).json({ ok: false, error: message });
  }
});

restoreRouter.post("/", async (req, res) => {
  let target;
  try {
    target = parseTarget(req.body);
  } catch (err) {
    res.status(400).json({ error: err instanceof Error ? err.message : String(err) });
    return;
  }

  const { bucketId, fileName, sizeBytes, dropExisting } = req.body ?? {};
  if (typeof bucketId !== "string" || !bucketId || typeof fileName !== "string" || !fileName) {
    res.status(400).json({ error: "bucketId e fileName são obrigatórios" });
    return;
  }

  // The existence check runs here so the UI can ask about overwriting before a job is created.
  try {
    const { databaseExists } = await testConnection(target);
    if (databaseExists && dropExisting !== true) {
      const err = new DatabaseExistsError(target.database);
      res.status(409).json({ error: err.message, code: err.code });
      return;
    }
  } catch (err) {
    res.status(502).json({ error: err instanceof Error ? err.message : String(err) });
    return;
  }

  const job = startRestore({
    bucketId,
    fileName,
    sizeBytes: Number(sizeBytes) || 0,
    target,
    dropExisting: dropExisting === true,
  });

  res.status(202).json({ job });
});

restoreRouter.get("/:jobId", (req, res) => {
  const job = getJob(req.params.jobId);
  if (!job) {
    res.status(404).json({ error: "Job de restauração não encontrado" });
    return;
  }
  res.json({ job });
});
