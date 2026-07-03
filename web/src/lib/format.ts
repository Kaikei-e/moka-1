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
