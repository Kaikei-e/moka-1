import { startService } from './support/compose';

// 安全網。`12-rag-failsoft.spec.ts` は e2e-llm-mock を意図的に止めるので afterAll で必ず
// 再開する — が、**afterAll も teardown プロジェクトも Ctrl-C(SIGINT)では走らない**。
// globalTeardown だけは SIGINT でも走るので、旧 bash の `trap ... EXIT` に相当する保険を
// ここに置く。
//
// 冪等: 既に動いていれば no-op。スタックが上がっていない状況(`--list` だけ叩く等)でも
// 壊れないよう allowFail にしてある。
export default function globalTeardown(): void {
	startService('e2e-llm-mock', { allowFail: true, timeoutMs: 30_000 });
}
