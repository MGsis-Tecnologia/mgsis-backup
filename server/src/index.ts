import express from "express";
import cors from "cors";
import { config } from "./config.js";
import { bucketsRouter } from "./routes/buckets.js";
import { clientsRouter } from "./routes/clients.js";

const app = express();
app.use(cors());
app.use(express.json());

app.get("/api/health", (_req, res) => res.json({ ok: true }));
app.use("/api/buckets", bucketsRouter);
app.use("/api/clients", clientsRouter);

app.listen(config.port, () => {
  console.log(`Server listening on http://localhost:${config.port}`);
});
