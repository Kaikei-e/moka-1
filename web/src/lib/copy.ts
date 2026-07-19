// moka の声(DESIGN_LANGUAGE §7)。感嘆符・絵文字・謝罪・「成功しました」を使わない。
// 空状態は招待、エラーは事実 + 次の行動。

export const EMPTY_ARTICLES = 'まだ記事がありません。URL を貼るとここに並びます';
export const EMPTY_FEEDS = 'まだフィードがありません。URL を貼るとここに並びます';
export const PICK_ARTICLE = '一覧から記事を選ぶと、ここに運ばれます';
export const UNTRANSLATED = 'まだ訳されていません';
export const REGISTERED = '登録しました';
export const LIST_UNAVAILABLE = '記事を読み込めませんでした。再読み込みしてください';
export const FETCH_FULLTEXT = '全文を取り寄せる';
export const FETCHING_FULLTEXT = '取り寄せています';
export const SUMMARIZING = '要約しています';
export const SUMMARIZE = '要約する';
export const RETRY_SUMMARIZE = '再試行する';
export const REGENERATE_SUMMARY = '要約をやり直す';
export const EXTRACTING_TAGS = '抽出しています';
export const EXTRACT_TAGS = 'タグを付ける';
export const RETRY_EXTRACT_TAGS = '再試行する';
export const LOADING_MORE = '続きを読み込んでいます';
export const LOAD_MORE_FAILED = '続きを読み込めませんでした';
export const RETRY_LOAD_MORE = '再試行する';

// 検索(サイドバー)。検索は道具 — 記事が主役なので、待ちも結果も静かに
export const SEARCH_LABEL = '記事を探す';
export const SEARCH_PLACEHOLDER = '記事を探す…';
export const SEARCHING = '探しています';
export const SEARCH_EMPTY = '見つかりませんでした';
export const SEARCH_FAILED = '探せませんでした。再試行してください';

// 問い返し(訊く)。待ちは工程コピー(§7.1 prefill = 蒸らし)、出典は回答の下に小さく
export const ANSWERING = '蒸らしています';
export const ASK_FAILED = '答えられませんでした。訊き直してください';
export const ASK_EMPTY_QUESTION = '質問を入力してください';
export const SOURCES_LABEL = '出典';

// パスキー(/auth/login、ADR00021)。給仕の声 — 技術用語(WebAuthn 等)を出さず、
// 失敗は事実 + 次の行動(§7.2)。待ちはドリップ + 鍵の工程コピーで静かに
export const AUTH_WELCOME_NEW = 'はじめまして。この moka はあなたのものです';
export const AUTH_WELCOME_NEW_HINT = 'パスキーを作ると、この店の鍵はあなただけのものになります';
export const AUTH_WELCOME_BACK = 'おかえりなさい';
export const AUTH_WELCOME_BACK_HINT = 'パスキーで鍵を開けてください';
export const AUTH_CREATE_PASSKEY = 'パスキーを作る';
export const AUTH_UNLOCK = 'パスキーで開ける';
export const AUTH_CREATING_KEY = '鍵を作っています';
export const AUTH_VERIFYING_KEY = '鍵を確かめています';
export const AUTH_OPENED = '開きました';
export const AUTH_REGISTER_FAILED = 'パスキーを作れませんでした。もう一度試してください';
export const AUTH_LOGIN_FAILED = '鍵を開けられませんでした。もう一度試してください';
export const AUTH_ALREADY_REGISTERED = 'パスキーは既にあります。再読み込みしてください';
export const AUTH_NOT_REGISTERED = 'パスキーがまだありません。再読み込みしてください';
export const AUTH_STATUS_UNAVAILABLE = '鍵の状態を確かめられませんでした。再読み込みしてください';

// ホームの空状態: 招待 + 今日のハイライトの静的予告(一文だけ。進行表現・操作は付けない)
export const HIGHLIGHT_FORECAST = 'やがて毎朝、今日読むべき記事がここに揃います';

// フィードの削除(店との別れ)。破壊的なので警告様式(§2.4)の二段確認を挟む
export const DELETE_FEED = '削除する';
export const DELETE_CANCEL = 'やめる';
export const DELETE_CONFIRM_LABEL = '削除の確認';
export const DELETE_FEED_WARNING =
	'このフィードを削除すると、届いた記事・要約・既読の記録もすべて消えます。元に戻せません';
export const DELETED = '削除しました';
