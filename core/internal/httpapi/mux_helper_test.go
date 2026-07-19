package httpapi

import "net/http"

// muxDeps は NewMux の全ポートをテスト用に束ねる。ゼロ値のフィールドは何もしない
// フェイクで埋める — 各テストは関心のあるポートだけを差し替える(ポートを追加する
// たびに全テストの NewMux 呼び出しが壊れるのを防ぐ。本体の合成は main.go の責務のまま)。
type muxDeps struct {
	feeds         FeedRegistrar
	feedList      FeedLister
	feedDelete    FeedDeleter
	articles      ArticleLister
	article       ArticleGetter
	reads         ArticleReadMarker
	fullTexts     FullTextFetcher
	summarizer    ArticleSummarizer
	summaryReader SummaryReader
	tagger        ArticleTagger
	tagsReader    TagsReader
}

// newTestMux は未指定ポートをデフォルトのフェイクで補って NewMux を組み立てる。
func newTestMux(d muxDeps) *http.ServeMux {
	if d.feeds == nil {
		d.feeds = &fakeRegistrar{}
	}
	if d.feedList == nil {
		d.feedList = &fakeFeedLister{}
	}
	if d.feedDelete == nil {
		d.feedDelete = &fakeFeedDeleter{}
	}
	if d.articles == nil {
		d.articles = &fakeLister{}
	}
	if d.article == nil {
		d.article = &fakeGetter{}
	}
	if d.reads == nil {
		d.reads = &fakeReadMarker{}
	}
	if d.fullTexts == nil {
		d.fullTexts = &fakeFullTextFetcher{}
	}
	if d.summarizer == nil {
		d.summarizer = &fakeSummarizer{}
	}
	if d.summaryReader == nil {
		d.summaryReader = &fakeSummaryReader{}
	}
	if d.tagger == nil {
		d.tagger = &fakeArticleTagger{}
	}
	if d.tagsReader == nil {
		d.tagsReader = &fakeTagsReader{}
	}
	return NewMux(
		d.feeds, d.feedList, d.feedDelete, d.articles, d.article, d.reads, d.fullTexts,
		d.summarizer, d.summaryReader, d.tagger, d.tagsReader,
	)
}
