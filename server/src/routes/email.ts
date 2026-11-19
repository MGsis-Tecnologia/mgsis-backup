import express from "express";
import { sendTestEmail } from "../services/emailService.js";

export const emailRouter = express.Router();

emailRouter.post("/test", async (req, res) => {
  try {
    const { toEmail, subject, htmlContent } = req.body;

    if (!toEmail || !subject || !htmlContent) {
      return res.status(400).json({
        error: "Missing required fields: toEmail, subject, htmlContent",
      });
    }

    const result = await sendTestEmail(toEmail, subject, htmlContent);
    res.json({ success: true, messageId: result.messageId });
  } catch (error) {
    res.status(500).json({
      error: error instanceof Error ? error.message : "Unknown error",
    });
  }
});
