// moka の声(DESIGN_LANGUAGE §7)。感嘆符・絵文字・謝罪・「成功しました」を使わない。
// 空状態は招待、エラーは事実 + 次の行動。

export const EMPTY_ARTICLES = 'まだ記事がありません。URL を貼るとここに並びます';
export const EMPTY_FEEDS = 'まだフィードがありません。URL を貼るとここに並びます';
export const PICK_ARTICLE = '一覧から記事を選ぶと、ここに運ばれます';
export const ANSWER_PENDING = '回答の準備ができていません';
export const UNTRANSLATED = 'まだ訳されていません';
export const REGISTERED = '登録しました';
export const LIST_UNAVAILABLE = '記事を読み込めませんでした。再読み込みしてください';
export const FETCH_FULLTEXT = '全文を取り寄せる';
export const FETCHING_FULLTEXT = '取り寄せています';
export const SUMMARIZING = '要約しています';
export const SUMMARIZE = '要約する';
export const RETRY_SUMMARIZE = '再試行する';
export const REGENERATE_SUMMARY = '要約をやり直す';
export const LOADING_MORE = '続きを読み込んでいます';
export const LOAD_MORE_FAILED = '続きを読み込めませんでした';
export const RETRY_LOAD_MORE = '再試行する';

// ホームの空状態: 招待 + 今日のハイライトの静的予告(一文だけ。進行表現・操作は付けない)
export const HIGHLIGHT_FORECAST = 'やがて毎朝、今日読むべき記事がここに揃います';

// フィードの削除(店との別れ)。破壊的なので警告様式(§2.4)の二段確認を挟む
export const DELETE_FEED = '削除する';
export const DELETE_CANCEL = 'やめる';
export const DELETE_CONFIRM_LABEL = '削除の確認';
export const DELETE_FEED_WARNING =
	'このフィードを削除すると、届いた記事・要約・既読の記録もすべて消えます。元に戻せません';
export const DELETED = '削除しました';
