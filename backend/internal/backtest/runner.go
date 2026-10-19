package backtest

import (
	"context"
	"errors"
	"hash/fnv"
	"log/slog"
	"sort"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/YLaguna506/tickerlens/backend/internal/edgar"
	"github.com/YLaguna506/tickerlens/backend/internal/prices"
	"github.com/YLaguna506/tickerlens/backend/internal/scoring"
	"github.com/YLaguna506/tickerlens/backend/internal/store"
)

// Source provides the raw data: SEC facts as filed (with their filing
// dates) and full price history.
type Source interface {
	Company(ctx context.Context, ticker string) (edgar.Company, error)
	Facts(ctx context.Context, cik string) (*edgar.CompanyFacts, error)
	Profile(ctx context.Context, cik string) (edgar.Profile, error)
	History(ctx context.Context, ticker string) ([]prices.Bar, error)
}

// Store keeps backtests and their results (store.Backtests).
type Store interface {
	Create(ctx context.Context, universe string, params store.BacktestParams, members []store.ScreenMember) (store.Backtest, error)
	Unfinished(ctx context.Context) ([]store.Backtest, error)
	SetStatus(ctx context.Context, id, status, errorText string) error
	SaveResult(ctx context.Context, id string, r store.BacktestResult) error
	Results(ctx context.Context, id string) ([]store.BacktestResult, error)
}

// Benchmark is what returns are measured against: the S&P 500 ETF, with
// dividends reinvested like the stocks'.
const Benchmark = "SPY"

// ErrBusy is returned when a backtest is already running.
var ErrBusy = errors.New("a backtest is already running")

// Runner runs backtests in the background, one at a time.
type Runner struct {
	Source  Source
	Store   Store
	Config  scoring.Config
	Logger  *slog.Logger
	Base    context.Context // cancelled on shutdown: a backtest then resumes on the next start
	Workers int             // companies evaluated at once
	Now     func() time.Time

	mu      sync.Mutex
	running map[string]context.CancelFunc
	wg      sync.WaitGroup
}

// Start creates a backtest and runs it in the background.
func (r *Runner) Start(ctx context.Context, universe string, members []store.ScreenMember) (store.Backtest, error) {
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	// The latest closes may be a few days old.
	params := store.BacktestParams{
		Dates:          Dates(now().AddDate(0, 0, -7).Format(time.DateOnly)),
		ScoringVersion: r.Config.Version,
		Benchmark:      Benchmark,
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.running) > 0 {
		return store.Backtest{}, ErrBusy
	}
	bt, err := r.Store.Create(ctx, universe, params, members)
	if err != nil {
		return store.Backtest{}, err
	}
	r.launch(bt)
	return bt, nil
}

// Resume continues the latest backtest a restart interrupted.
func (r *Runner) Resume(ctx context.Context) error {
	list, err := r.Store.Unfinished(ctx)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, bt := range list {
		if i < len(list)-1 || len(r.running) > 0 {
			_ = r.Store.SetStatus(ctx, bt.ID, "cancelled", "replaced by a newer backtest")
			continue
		}
		r.logger().Info("resuming backtest", "id", bt.ID)
		r.launch(bt)
	}
	return nil
}

// Cancel stops a running backtest; it reports whether it was running.
func (r *Runner) Cancel(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	cancel, ok := r.running[id]
	if ok {
		cancel()
	}
	return ok
}

// Running reports whether a backtest is running.
func (r *Runner) Running(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.running[id]
	return ok
}

// Wait blocks until every backtest has stopped.
func (r *Runner) Wait() { r.wg.Wait() }

func (r *Runner) launch(bt store.Backtest) {
	base := r.Base
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithCancel(base)
	if r.running == nil {
		r.running = map[string]context.CancelFunc{}
	}
	r.running[bt.ID] = cancel
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer func() {
			cancel()
			r.mu.Lock()
			delete(r.running, bt.ID)
			r.mu.Unlock()
		}()
		r.run(ctx, bt)
	}()
}

func (r *Runner) run(ctx context.Context, bt store.Backtest) {
	log := r.logger().With("backtest", bt.ID)
	finish := func(status, errorText string) {
		if status == "cancelled" && r.Base != nil && r.Base.Err() != nil {
			log.Info("backtest paused by shutdown")
			return
		}
		if err := r.Store.SetStatus(context.WithoutCancel(ctx), bt.ID, status, errorText); err != nil {
			log.Error("backtest status", "err", err)
		}
		log.Info("backtest finished", "status", status)
	}

	bench, err := r.Source.History(ctx, bt.Params.Benchmark)
	if err != nil {
		if ctx.Err() != nil {
			finish("cancelled", "")
		} else {
			finish("failed", "benchmark prices: "+err.Error())
		}
		return
	}
	saved, err := r.Store.Results(ctx, bt.ID)
	if err != nil {
		finish("failed", err.Error())
		return
	}
	done := map[string]bool{}
	for _, s := range saved {
		done[s.Ticker] = true
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(max(r.Workers, 1))
	for _, m := range bt.Members {
		if done[m.Ticker] {
			continue
		}
		g.Go(func() error {
			if gctx.Err() != nil {
				return gctx.Err()
			}
			res := r.company(gctx, m, bench, bt.Params.Dates)
			if gctx.Err() != nil {
				return gctx.Err()
			}
			return r.Store.SaveResult(gctx, bt.ID, res)
		})
	}
	if err := g.Wait(); err != nil {
		if ctx.Err() != nil {
			finish("cancelled", "")
		} else {
			finish("failed", err.Error())
		}
		return
	}
	finish("done", "")
}

// company evaluates one company at every date.
func (r *Runner) company(ctx context.Context, m store.ScreenMember, bench []prices.Bar, dates []string) store.BacktestResult {
	res := store.BacktestResult{Ticker: m.Ticker, Name: m.Name, Observations: []store.Observation{}}
	co, err := r.Source.Company(ctx, m.Ticker)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	if res.Name == "" {
		res.Name = co.Name
	}
	facts, err := r.Source.Facts(ctx, co.CIK)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	bars, err := r.Source.History(ctx, co.Ticker)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	c := Company{Company: co, Sector: m.Sector, Facts: facts, Bars: bars}
	if p, err := r.Source.Profile(ctx, co.CIK); err == nil && p.SIC != "" {
		c.Profile = &p
	}
	res.Observations = Evaluate(c, bench, dates, r.Config)
	return res
}

func (r *Runner) logger() *slog.Logger {
	if r.Logger == nil {
		return slog.New(slog.DiscardHandler)
	}
	return r.Logger
}

// Sample picks n members in a fixed pseudo-random order (by a hash of the
// ticker): spread across the list rather than its first letters, and the
// same companies every time, so their cached price history is reused.
func Sample(members []store.ScreenMember, n int) []store.ScreenMember {
	if n <= 0 || n >= len(members) {
		return members
	}
	h := func(t string) uint32 {
		f := fnv.New32a()
		_, _ = f.Write([]byte(t))
		return f.Sum32()
	}
	out := append([]store.ScreenMember(nil), members...)
	sort.Slice(out, func(i, j int) bool { return h(out[i].Ticker) < h(out[j].Ticker) })
	out = out[:n]
	sort.Slice(out, func(i, j int) bool { return out[i].Ticker < out[j].Ticker })
	return out
}

// SECAndTiingo is the Source behind the API: SEC facts through the EDGAR
// client, price history from Tiingo cached for a month (the backtest needs
// years of history, not today's close).
type SECAndTiingo struct {
	SEC    *edgar.Client
	Tiingo *prices.Tiingo // nil without a Tiingo key
}

// Company looks up a ticker.
func (s SECAndTiingo) Company(ctx context.Context, ticker string) (edgar.Company, error) {
	return s.SEC.LookupTicker(ctx, ticker)
}

// Facts returns the company's facts with their filing dates.
func (s SECAndTiingo) Facts(ctx context.Context, cik string) (*edgar.CompanyFacts, error) {
	return s.SEC.CompanyFacts(ctx, cik)
}

// Profile returns the company's industry code.
func (s SECAndTiingo) Profile(ctx context.Context, cik string) (edgar.Profile, error) {
	return s.SEC.Profile(ctx, cik)
}

// History returns daily prices since HistoryStart.
func (s SECAndTiingo) History(ctx context.Context, ticker string) ([]prices.Bar, error) {
	if s.Tiingo == nil {
		return nil, errors.New("prices: TIINGO_API_KEY not set")
	}
	return s.Tiingo.History(ctx, ticker, HistoryStart, 30*24*time.Hour)
}
