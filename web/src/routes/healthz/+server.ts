// エッジ(Plecto)と compose healthcheck が叩く readiness プローブ。
// 契約: GET /healthz → 200(plecto/manifest.toml の upstream.health)
import type { RequestHandler } from './$types';

export const GET: RequestHandler = () => new Response('ok');
