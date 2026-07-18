// 記事リスト行のビューモデル(境界の Article → 表示用の形)。手がかりはフィード名と
// 相対日時のみ — 未読数・バッジは数えない(急かさない店、CONTEXT.md「既読」)。
// コンポーネントに埋めず lib に置くのは、純ロジックを node 環境の vitest で守るため
import type { Article } from '$lib/api/schemas';
import { formatRelativeTime } from './format';
import { hostnameOf } from './url';

export type ArticleListItem = {
	id: number;
	title: string;
	meta: string; // 「フィード名・相対日時」。どちらも無ければ空文字
	read: boolean;
};

export function toArticleListItem(article: Article, now: Date = new Date()): ArticleListItem {
	const source = article.feed_title ?? hostnameOf(article.url) ?? '';
	const when = formatRelativeTime(article.published_at ?? article.created_at, now);
	return {
		id: article.id,
		title: article.title,
		meta: [source, when].filter(Boolean).join('・'),
		read: article.read
	};
}
