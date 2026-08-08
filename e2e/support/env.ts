// E2E 実行環境の解決。既定値は e2e/README.md の手順(compose.e2e.yaml オーバーレイで
// moka-core を 127.0.0.1:8080、Plecto を 443 に公開した状態)に揃えてある。
import path from 'node:path';

/** リポジトリルート(compose.yaml / compose.e2e.yaml / secrets/ の親)。 */
export const repoRoot = path.resolve(import.meta.dirname, '..', '..');

/** e2e/ ディレクトリ(fixtures/ mock-llm/ の親)。 */
export const e2eRoot = path.resolve(import.meta.dirname, '..');

/** moka-core の HTTP API(compose.e2e.yaml が 127.0.0.1:8080 に公開する)。 */
export const coreBaseURL = process.env.E2E_HOST ?? 'http://localhost:8080';

/** Plecto エッジ(本番スタック側、自己署名 TLS)。 */
export const edgeBaseURL = process.env.EDGE_HOST ?? 'https://localhost';

/**
 * フィクスチャ配信 nginx の origin。moka-core が docker ネットワーク内で解決するので
 * ホスト側の名前解決は不要(テストランナーはこの URL を自分では叩かない)。
 */
export const fixtureOrigin = process.env.E2E_FIXTURE_ORIGIN ?? 'http://e2e-fixtures';

/** 配信中フィクスチャの URL。名前は e2e/fixtures/ 配下のファイル名。 */
export function fixtureURL(fileName: string): string {
	return `${fixtureOrigin}/${fileName}`;
}

/**
 * 各シナリオが使うフィクスチャ。`-v1` / `-v2` を持つものは配信実体
 * (`served`)へコピーして差し替える(fixture-files.ts)。
 */
export const fixtures = {
	/** 本体フィクスチャ(25記事)。 */
	main: 'feed.xml',
	/** published_at が NULL の記事を含む3記事。 */
	nullPubDate: 'feed-null-pubdate.xml',
	/** enrich.Scheduler の自動濃縮検証用(1記事)。 */
	enrich: 'feed-enrich.xml',
	/** Q&A 文脈クエリ配線検証用(同一タイトル・別内容の2記事)。 */
	qaContext: 'feed-qa-context.xml',
	/** 非304再取得検証用。served へ v1 / v2 をコピーして配信する。 */
	dedupe: { served: 'feed-dedupe.xml', v1: 'feed-dedupe-v1.xml', v2: 'feed-dedupe-v2.xml' },
	/** 常駐スケジューラ検証用。served へ v1 / v2 をコピーして配信する。 */
	scheduler: {
		served: 'feed-scheduler.xml',
		v1: 'feed-scheduler-v1.xml',
		v2: 'feed-scheduler-v2.xml'
	}
} as const;
