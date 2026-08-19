import B2 from "backblaze-b2";
import { config } from "../config.js";

const b2 = new B2({
  applicationKeyId: config.b2.applicationKeyId,
  applicationKey: config.b2.applicationKey,
});

let authorizedAt = 0;
const AUTH_TTL_MS = 20 * 60 * 1000; // B2 auth tokens are valid for 24h; re-auth well before that

export async function getAuthorizedB2(): Promise<B2> {
  const now = Date.now();
  if (now - authorizedAt > AUTH_TTL_MS) {
    await b2.authorize();
    authorizedAt = now;
  }
  return b2;
}
