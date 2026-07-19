package llm

import "strings"

// thinkStreamState は ThinkStreamStripper の内部フェーズ。
type thinkStreamState int

const (
	thinkStreamUndecided thinkStreamState = iota
	thinkStreamInsideThink
	thinkStreamPassthrough
)

// ThinkStreamStripper は StripThink のストリーミング版: チャンク到着のたびに
// 「<think> ブロックの外側」だけを即座に返す。<think> が閉じるかどうか判明するまで
// 何もクライアントへ流さない(ADR00014 §5 の防御的除去をチャンク単位に適用したもの)。
// 最終的な保存判定は常に StripThink(完全な生テキスト) を単一の正とし、これは
// リアルタイム表示専用の補助ロジック。summarize と rag(Q&A)が共用する
// (bp-go: think 除去は llm パッケージに一元化)。
type ThinkStreamStripper struct {
	state   thinkStreamState
	pending strings.Builder
}

// Feed は生チャンクを1つ処理し、今すぐクライアントへ流してよい文字列を返す。
func (s *ThinkStreamStripper) Feed(chunk string) string {
	switch s.state {
	case thinkStreamPassthrough:
		return chunk
	case thinkStreamInsideThink:
		s.pending.WriteString(chunk)
		buffered := s.pending.String()
		_, after, ok := strings.Cut(buffered, CloseTag)
		if !ok {
			return ""
		}
		s.pending.Reset()
		s.state = thinkStreamPassthrough
		return after
	default: // thinkStreamUndecided
		s.pending.WriteString(chunk)
		buffered := s.pending.String()
		// StripThink と同じ判定: 先頭空白は許容した上で、冒頭が <think> の時だけ think モード。
		// 先頭空白をスキップしないと "\n<think>" で passthrough に落ち、CoT が丸ごと漏れる。
		trimmed := strings.TrimLeft(buffered, ThinkLeadingSpace)
		if len(trimmed) < len(OpenTag) {
			if strings.HasPrefix(OpenTag, trimmed) {
				return "" // まだ <think> かどうか確定しない — 保留
			}
			s.pending.Reset()
			s.state = thinkStreamPassthrough
			return buffered
		}
		if !strings.HasPrefix(trimmed, OpenTag) {
			s.pending.Reset()
			s.state = thinkStreamPassthrough
			return buffered
		}
		rest := trimmed[len(OpenTag):]
		s.pending.Reset()
		s.state = thinkStreamInsideThink
		_, after, ok := strings.Cut(rest, CloseTag)
		if !ok {
			s.pending.WriteString(rest)
			return ""
		}
		s.state = thinkStreamPassthrough
		return after
	}
}

// Finish は完全な応答終端(finish_reason=stop相当)で呼ぶ。未決着のまま残った
// バッファは <think> の開始タグ長にすら満たない = think タグではあり得ないので
// 素直な本文として flush する。closed=false は think タグが閉じずに終わった場合。
func (s *ThinkStreamStripper) Finish() (flush string, closed bool) {
	if s.state == thinkStreamUndecided && s.pending.Len() > 0 {
		flush = s.pending.String()
		s.pending.Reset()
		s.state = thinkStreamPassthrough
	}
	return flush, s.state != thinkStreamInsideThink
}
