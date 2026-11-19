import express from "express";
import cors from "cors";
import { config } from "./config.js";
import { bucketsRouter } from "./routes/buckets.js";
import { restoreRouter } from "./routes/restore.js";
import { emailRouter } from "./routes/email.js";
import { wsConfigRouter } from "./routes/wsConfig.js";

const app = express();
app.use(cors());
app.use(express.json());

app.get("/api/health", (_req, res) => res.json({ ok: true }));
app.use("/api/buckets", bucketsRouter);
app.use("/api/restore", restoreRouter);
app.use("/api/email", emailRouter);
app.use("/api/ws-config", wsConfigRouter);
app.get("/api/restore-defaults", (_req, res) => res.json(config.restoreDefaults));

app.listen(config.port, "127.0.0.1", () => {
  console.log(`Server listening on http://127.0.0.1:${config.port}`);
});
