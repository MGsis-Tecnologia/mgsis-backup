const NAMESPACE = "urn:WSMGSIS";
const TIMEOUT_MS = 30_000;

const XML_ENTITIES: Record<string, string> = { lt: "<", gt: ">", amp: "&", apos: "'", quot: '"' };

function escapeXml(value: string): string {
  return value.replace(/[<>&'"]/g, (ch) => `&#${ch.charCodeAt(0)};`);
}

function decodeXml(value: string): string {
  return value.replace(/&(#x[0-9a-fA-F]+|#\d+|[a-z]+);/g, (match, entity: string) => {
    if (entity.startsWith("#x")) return String.fromCodePoint(parseInt(entity.slice(2), 16));
    if (entity.startsWith("#")) return String.fromCodePoint(Number(entity.slice(1)));
    return XML_ENTITIES[entity] ?? match;
  });
}

function buildEnvelope(params: Record<string, string | number>): string {
  const fields = Object.entries(params)
    .map(([name, value]) =>
      typeof value === "number"
        ? `<${name} xsd:type="xsd:int" xmlns="${NAMESPACE}">${value}</${name}>`
        : `<${name} xsd:type="xsd:string" xmlns="${NAMESPACE}">${escapeXml(value)}</${name}>`,
    )
    .join("");
  return (
    `<?xml version="1.0" encoding="UTF-8"?>` +
    `<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" ` +
    `xmlns:xsi="http://www.w3.org/1999/XMLSchema-instance" xmlns:xsd="http://www.w3.org/1999/XMLSchema">` +
    `<soap:Body>${fields}</soap:Body>` +
    `</soap:Envelope>`
  );
}

export async function callMgsisOperation(
  url: string,
  operation: string,
  params: Record<string, string | number>,
): Promise<string[]> {
  const endpoint = url.replace(/\?wsdl$/i, "");
  const referer = endpoint.replace(/[^/]*$/, `${operation}.htm`);
  const body = new URLSearchParams({
    xml: buildEnvelope(params),
    action: `${NAMESPACE}/${operation}`,
  }).toString();

  let response: Response;
  try {
    response = await fetch(endpoint, {
      method: "POST",
      headers: {
        "Content-Type": "application/x-www-form-urlencoded",
        Referer: referer,
      },
      body,
      signal: AbortSignal.timeout(TIMEOUT_MS),
    });
  } catch (err) {
    const reason = err instanceof Error ? err.message : String(err);
    throw new Error(`Não foi possível conectar ao WebService: ${reason}`);
  }

  const xml = await response.text();

  const fault = /<(?:\w+:)?faultstring[^>]*>([\s\S]*?)<\/(?:\w+:)?faultstring>/.exec(xml);
  if (fault) throw new Error(`WebService retornou erro: ${decodeXml(fault[1].trim())}`);
  if (!response.ok) throw new Error(`WebService respondeu HTTP ${response.status}`);

  const resultPattern = new RegExp(
    `<(?:\\w+:)?${operation}Result[^>]*>([\\s\\S]*?)<\\/(?:\\w+:)?${operation}Result>`,
    "g",
  );
  const results = [...xml.matchAll(resultPattern)].map((m) => decodeXml(m[1]));
  if (results.length === 0) throw new Error("Resposta inesperada do WebService (sem retorno da operação)");
  return results;
}
