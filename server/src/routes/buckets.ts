import { Router } from "express";
import { getFileDownloadUrl, listBucketFiles, listBucketsWithStats } from "../services/bucketService.js";

export const bucketsRouter = Router();

bucketsRouter.get("/", async (req, res) => {
  try {
    const forceRefresh = req.query.refresh === "true";
    const buckets = await listBucketsWithStats(forceRefresh);
    res.json({ buckets });
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    console.error("Failed to list buckets:", message);
    res.status(502).json({ error: "Failed to fetch data from Backblaze B2" });
  }
});

bucketsRouter.get("/:bucketId/download", async (req, res) => {
  try {
    const fileName = req.query.fileName;
    if (typeof fileName !== "string" || !fileName) {
      res.status(400).json({ error: "Missing fileName query parameter" });
      return;
    }
    const url = await getFileDownloadUrl(req.params.bucketId, fileName);
    res.json({ url });
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    console.error("Failed to create download URL:", message);
    res.status(502).json({ error: "Failed to fetch data from Backblaze B2" });
  }
});

bucketsRouter.get("/:bucketId/files", async (req, res) => {
  try {
    const forceRefresh = req.query.refresh === "true";
    const files = await listBucketFiles(req.params.bucketId, forceRefresh);
    res.json({ files });
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    console.error("Failed to list files:", message);
    res.status(502).json({ error: "Failed to fetch data from Backblaze B2" });
  }
});
