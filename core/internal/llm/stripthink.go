package llm

import "strings"

// think タグの開閉境界。StripThink と ThinkStreamStripper の両方が参照する。
const (
	OpenTag  = "<think>"
	CloseTag = "</think>"
	// ThinkLeadingSpace は think タグ検出時に無視する応答冒頭の空白類(Qwen は
	// "\n<think>" のように改行を先行させることがある)。
	ThinkLeadingSpace = " \t\r\n"
)

// StripThink は Qwen 系モデルが(flag が効かなかった場合の防御として)応答冒頭に付ける
// <think>...</think> CoT を機械的に剥がす。think タグは応答冒頭(先頭空白は許容)のみを
// 対象とする — 本文途中の "<think>" は引用等の本文とみなし、ThinkStreamStripper の
// ストリーミング判定とも一致させる(片方だけ途中一致だと、ストリームで見えた本文と
// 保存される本文が食い違う)。閉じずに truncate された場合は closed=false
// (呼び出し元はエラー扱いにすべき)。
func StripThink(raw string) (text string, stripped bool, closed bool) {
	rest, ok := strings.CutPrefix(strings.TrimLeft(raw, ThinkLeadingSpace), OpenTag)
	if !ok {
		return strings.TrimSpace(raw), false, true
	}

	_, after, ok := strings.Cut(rest, CloseTag)
	if !ok {
		return "", true, false
	}

	return strings.TrimSpace(after), true, true
}
