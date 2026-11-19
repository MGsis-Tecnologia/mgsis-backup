import { useState } from "react";
import { sendTestEmail } from "../api.js";

interface EmailTestModalProps {
  isOpen: boolean;
  onClose: () => void;
}

export function EmailTestModal({ isOpen, onClose }: EmailTestModalProps) {
  const [toEmail, setToEmail] = useState("");
  const [subject, setSubject] = useState("Teste de Email - Backup não OK");
  const [htmlContent, setHtmlContent] = useState(
    `<h2>Aviso: Backup não está OK</h2>
<p>Um dos seus backups não está funcionando corretamente.</p>
<p>Por favor, verifique o status e tome as ações necessárias.</p>`
  );
  const [sending, setSending] = useState(false);
  const [message, setMessage] = useState<{ type: "success" | "error"; text: string } | null>(null);

  async function handleSend() {
    if (!toEmail || !subject || !htmlContent) {
      setMessage({ type: "error", text: "Preencha todos os campos" });
      return;
    }

    setSending(true);
    setMessage(null);
    try {
      const result = await sendTestEmail(toEmail, subject, htmlContent);
      setMessage({ type: "success", text: `Email enviado com sucesso! ID: ${result.messageId}` });
      setTimeout(() => {
        onClose();
        setToEmail("");
        setSubject("Teste de Email - Backup não OK");
        setHtmlContent(
          `<h2>Aviso: Backup não está OK</h2>
<p>Um dos seus backups não está funcionando corretamente.</p>
<p>Por favor, verifique o status e tome as ações necessárias.</p>`
        );
        setMessage(null);
      }, 2000);
    } catch (error) {
      setMessage({ type: "error", text: error instanceof Error ? error.message : "Erro desconhecido" });
    } finally {
      setSending(false);
    }
  }

  if (!isOpen) return null;

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal-content" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <h2>Testar Envio de Email</h2>
          <button className="modal-close" onClick={onClose}>
            ✕
          </button>
        </div>

        <div className="modal-body">
          <div className="form-group">
            <label htmlFor="toEmail">Email para enviar:</label>
            <input
              id="toEmail"
              type="email"
              placeholder="seu.email@example.com"
              value={toEmail}
              onChange={(e) => setToEmail(e.target.value)}
              disabled={sending}
            />
          </div>

          <div className="form-group">
            <label htmlFor="subject">Assunto:</label>
            <input
              id="subject"
              type="text"
              value={subject}
              onChange={(e) => setSubject(e.target.value)}
              disabled={sending}
            />
          </div>

          <div className="form-group">
            <label htmlFor="htmlContent">Conteúdo (HTML):</label>
            <textarea
              id="htmlContent"
              value={htmlContent}
              onChange={(e) => setHtmlContent(e.target.value)}
              disabled={sending}
              rows={8}
            />
          </div>

          {message && <p className={`message ${message.type}`}>{message.text}</p>}

          <div className="modal-actions">
            <button onClick={onClose} disabled={sending}>
              Cancelar
            </button>
            <button onClick={handleSend} disabled={sending} className="primary">
              {sending ? "Enviando…" : "Enviar Email"}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
