import nodemailer from "nodemailer";
import { config } from "../config.js";

const transporter = nodemailer.createTransport({
  host: config.ses.smtpHost,
  port: config.ses.smtpPort,
  secure: false,
  auth: {
    user: config.ses.smtpUser,
    pass: config.ses.smtpPassword,
  },
});

export async function sendTestEmail(toEmail: string, subject: string, htmlContent: string) {
  try {
    const result = await transporter.sendMail({
      from: config.ses.fromEmail,
      to: toEmail,
      subject,
      html: htmlContent,
    });
    return { success: true, messageId: result.messageId };
  } catch (error) {
    throw new Error(`Falha ao enviar email: ${error instanceof Error ? error.message : String(error)}`);
  }
}
