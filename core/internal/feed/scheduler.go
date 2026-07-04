package feed

import (
	"context"
	"log/slog"
	"time"

	"golang.org/x/time/rate"
)

// defaultTick は due 判定のポーリング間隔の既定値。
const defaultTick = 60 * time.Second

// minFetchInterval は外部リクエスト間隔の下限(tenets §3.2、§8 未決事項5)。
// スケジューラ起点の連続取得にのみ課す — ユーザー起点の同期登録(POST /api/v1/feeds)は
// 1リクエストなのでバーストの懸念が無く、対象にしない。
const minFetchInterval = 5 * time.Second

// DueFeedLister は「取得すべきフィード」一覧の消費側ポート(具象は *store.Store)。
type DueFeedLister interface {
	DueFeeds(ctx context.Context) ([]Feed, error)
}

// registerer は Scheduler が呼ぶ登録ユースケース(具象は *Registrar)。
type registerer interface {
	Register(ctx context.Context, rawURL string) (RegisterResult, error)
}

// Scheduler は常駐エージェントループの step1-2(tenets §3.2)を担う単一 goroutine の
// ティッカーループ。due なフィードを順に Registrar.Register へ委譲するだけで、
// 取得ロジック自体は持たない。
type Scheduler struct {
	due     DueFeedLister
	reg     registerer
	tick    time.Duration
	limiter *rate.Limiter
	log     *slog.Logger
}

// NewScheduler はスケジューラを組む。tick が 0 以下なら既定値(60秒)を使う。
func NewScheduler(due DueFeedLister, reg registerer, tick time.Duration, log *slog.Logger) *Scheduler {
	if tick <= 0 {
		tick = defaultTick
	}
	return &Scheduler{
		due:     due,
		reg:     reg,
		tick:    tick,
		limiter: rate.NewLimiter(rate.Every(minFetchInterval), 1),
		log:     log,
	}
}

// Run は ctx が cancel されるまでブロックする(main が goroutine で起動する)。
func (s *Scheduler) Run(ctx context.Context) {
	s.tickOnce(ctx) // 起動直後に一度 due 判定する(再起動後の取りこぼしを溜めない)

	ticker := time.NewTicker(s.tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tickOnce(ctx)
		}
	}
}

// tickOnce は due なフィードを1周ぶん取得する。1件のエラーは記録して次のフィードへ進む
// (フェイルソフト — 1フィードの障害で他フィードの取得を止めない)。
func (s *Scheduler) tickOnce(ctx context.Context) {
	feeds, err := s.due.DueFeeds(ctx)
	if err != nil {
		s.log.Error("list due feeds", "err", err.Error())
		return
	}
	for _, f := range feeds {
		if ctx.Err() != nil {
			return
		}
		if err := s.limiter.Wait(ctx); err != nil {
			return // ctx cancel 中
		}
		if _, err := s.reg.Register(ctx, f.URL); err != nil {
			s.log.Warn("scheduled fetch failed", "feed_id", f.ID, "url", f.URL, "err", err.Error())
		}
	}
}
