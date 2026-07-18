// 表示用の日付整形。実行環境のタイムゾーンに依存しないよう Asia/Tokyo に固定する
const dateFormat = new Intl.DateTimeFormat('ja-JP', {
	timeZone: 'Asia/Tokyo',
	year: 'numeric',
	month: 'long',
	day: 'numeric'
});

export function formatDate(iso: string | null): string {
	if (!iso) return '';
	const d = new Date(iso);
	if (Number.isNaN(d.getTime())) return '';
	return dateFormat.format(d);
}

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

// 記事リスト行の相対日時。急かさない声(感嘆符なし)で「どれくらい前か」だけを静かに示し、
// おおむね1週間を超えたら絶対日付に切り替える(相対表現が意味を失うため)
export function formatRelativeTime(iso: string | null, now: Date = new Date()): string {
	if (!iso) return '';
	const d = new Date(iso);
	if (Number.isNaN(d.getTime())) return '';
	const diff = now.getTime() - d.getTime();
	if (diff < MINUTE) return 'たった今'; // 未来の時刻(時計ずれ)もここに丸める
	if (diff < HOUR) return `${Math.floor(diff / MINUTE)}分前`;
	if (diff < DAY) return `${Math.floor(diff / HOUR)}時間前`;
	const days = Math.floor(diff / DAY);
	if (days === 1) return '昨日';
	if (days <= 7) return `${days}日前`;
	return formatDate(iso);
}
