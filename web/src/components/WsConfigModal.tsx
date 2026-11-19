import { useEffect, useState } from "react";
import { fetchWsConfig, saveWsConfig, testWsConnection } from "../api.js";
import type { WsTestResult } from "../types.js";

export function WsConfigModal({ onClose }: { onClose: () => void }) {
  const [url, setUrl] = useState("");
  const [token, setToken] = useState("");
  const [cnpj, setCnpj] = useState("");
  const [hasToken, setHasToken] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [message, setMessage] = useState<{ type: "success" | "error"; text: string } | null>(null);
  const [testResult, setTestResult] = useState<WsTestResult | null>(null);

  useEffect(() => {
    fetchWsConfig()
      .then((cfg) => {
        setUrl(cfg.url);
        setCnpj(cfg.cnpj);
        setHasToken(cfg.hasToken);
      })
      .catch((err) => setMessage({ type: "error", text: err instanceof Error ? err.message : "Erro ao carregar" }))
      .finally(() => setLoading(false));
  }, []);

  async function handleSave() {
    setSaving(true);
    setMessage(null);
    setTestResult(null);
    try {
      await saveWsConfig({ url, token, cnpj });
      setHasToken(true);
      setToken("");
      setMessage({ type: "success", text: "Configuração salva." });
    } catch (err) {
      setMessage({ type: "error", text: err instanceof Error ? err.message : "Erro ao salvar" });
    } finally {
      setSaving(false);
    }
  }

  async function handleTest() {
    setTesting(true);
    setMessage(null);
    setTestResult(null);
    try {
      setTestResult(await testWsConnection());
    } catch (err) {
      setMessage({ type: "error", text: err instanceof Error ? err.message : "Erro ao testar a conexão" });
    } finally {
      setTesting(false);
    }
  }

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal-panel restore-panel" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <h2>Configuração WebService</h2>
          <button type="button" className="modal-close" onClick={onClose} aria-label="Fechar">
            ×
          </button>
        </div>

        <p className="restore-subtitle">
          Dados de acesso ao WebService SOAP da MGSIS (usado para listar os clientes).
        </p>

        {loading ? (
          <p>Carregando…</p>
        ) : (
          <>
            <label className="form-field">
              <span>Endereço (URL do WebService)</span>
              <input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="http://.../WSMGSIS.awws" />
            </label>

            <label className="form-field">
              <span>Token</span>
              <input
                type="password"
                value={token}
                onChange={(e) => setToken(e.target.value)}
                placeholder={hasToken ? "•••••••• (deixe em branco para manter)" : "Token de acesso"}
              />
              {hasToken && <small className="muted-line">Já existe um token salvo. Preencha apenas para trocá-lo.</small>}
            </label>

            <label className="form-field">
              <span>CNPJ da Empresa (Identificação)</span>
              <input value={cnpj} onChange={(e) => setCnpj(e.target.value)} placeholder="00.000.000/0000-00" />
            </label>

            {message && <p className={message.type === "success" ? "test-ok" : "error"}>{message.text}</p>}

            {testResult?.ok && (
              <>
                <p className="test-ok">Conexão OK. Resposta do WebService:</p>
                <pre className="ws-result">{testResult.result?.join("\n") || "(vazia)"}</pre>
              </>
            )}
            {testResult && !testResult.ok && <p className="error">{testResult.error}</p>}

            <p className="muted-line">O teste de conexão usa a configuração já salva.</p>

            <div className="modal-footer">
              <button type="button" onClick={handleTest} disabled={testing || saving}>
                {testing ? "Testando…" : "Testar Conexão"}
              </button>
              <button type="button" className="btn-primary" onClick={handleSave} disabled={saving || testing}>
                {saving ? "Salvando…" : "Salvar Configuração"}
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  );
}
