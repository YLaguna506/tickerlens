// Package screener scans a list of companies (the S&P 500, the watchlist,
// any tickers) with the part of TickerLens that needs no AI, to find
// candidates worth a full report: for example, a strong business that is
// cheap against its own history.
//
// It runs in two stages because of the free data tiers. First the
// fundamentals of every company, from the SEC (fast and free): growth and
// financial health scores make a business score B without the AI's quality,
// moat and risk judgments. Then prices, only for the businesses that clear
// MinBusiness, strongest first and at most MaxPriced of them: the price feed
// allows 50 requests an hour and 500 different symbols a month. Prices give
// the valuation score V and where today's price sits in its own history.
//
// Results are saved company by company, so a scan resumes where it stopped
// when the API restarts.
package screener

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/YLaguna506/tickerlens/backend/internal/ingest"
	"github.com/YLaguna506/tickerlens/backend/internal/metrics"
	"github.com/YLaguna506/tickerlens/backend/internal/scoring"
	"github.com/YLaguna506/tickerlens/backend/internal/store"
)

// Fetcher gathers a company's data (ingest.Fetcher).
type Fetcher interface {
	Fetch(ctx context.Context, ticker string) (*ingest.Bundle, error)
}

// Store keeps screens and their results (store.Screens).
type Store interface {
	Create(ctx context.Context, universe string, params store.ScreenParams, members []store.ScreenMember) (store.Screen, error)
	Unfinished(ctx context.Context) ([]store.Screen, error)
	SetStatus(ctx context.Context, id, status, errorText string) error
	SaveResult(ctx context.Context, id string, r store.ScreenResult) error
	Results(ctx context.Context, id string) ([]store.ScreenResult, error)
}

// Stages of a result.
const (
	StageScanned = "scanned" // fundamentals read
	StageQueued  = "queued"  // waiting for its price
	StagePriced  = "priced"
	StageBelow   = "below"  // business score under MinBusiness: not priced
	StageCapped  = "capped" // over MaxPriced: not priced
	StageFailed  = "failed"
)

// Defaults for a screen's settings.
const (
	DefaultMinBusiness = 3.5
	DefaultMaxPriced   = 150
)

// ErrBusy is returned when a screen is already running: one at a time keeps
// the price feed's hourly allowance for it.
var ErrBusy = errors.New("a screen is already running")

// Screener runs screens in the background.
type Screener struct {
	Fundamentals Fetcher // SEC data only
	Priced       Fetcher // SEC data and prices
	Store        Store
	Config       scoring.Config
	Logger       *slog.Logger
	// Base is the context screens run under (cancelled on shutdown).
	Base context.Context
	// Workers is how many companies' fundamentals are read at once.
	Workers int

	mu      sync.Mutex
	running map[string]context.CancelFunc
	wg      sync.WaitGroup
}

// Start creates a screen and runs it in the background.
func (s *Screener) Start(ctx context.Context, universe string, members []store.ScreenMember, params store.ScreenParams) (store.Screen, error) {
	if params.MinBusiness <= 0 {
		params.MinBusiness = DefaultMinBusiness
	}
	if params.MaxPriced <= 0 {
		params.MaxPriced = DefaultMaxPriced
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.running) > 0 {
		return store.Screen{}, ErrBusy
	}
	sc, err := s.Store.Create(ctx, universe, params, members)
	if err != nil {
		return store.Screen{}, err
	}
	s.launch(sc)
	return sc, nil
}

// Resume continues the screens a restart interrupted.
func (s *Screener) Resume(ctx context.Context) error {
	list, err := s.Store.Unfinished(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, sc := range list {
		if i < len(list)-1 || len(s.running) > 0 {
			// Only the latest continues; one runs at a time.
			_ = s.Store.SetStatus(ctx, sc.ID, "cancelled", "replaced by a newer screen")
			continue
		}
		s.logger().Info("resuming screen", "id", sc.ID, "status", sc.Status)
		s.launch(sc)
	}
	return nil
}

// Cancel stops a running screen; it reports whether it was running.
func (s *Screener) Cancel(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	cancel, ok := s.running[id]
	if ok {
		cancel()
	}
	return ok
}

// Running reports whether a screen is running.
func (s *Screener) Running(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.running[id]
	return ok
}

// Wait blocks until every screen has stopped (after Base is cancelled).
func (s *Screener) Wait() { s.wg.Wait() }

// launch runs sc in a goroutine. The caller holds s.mu.
func (s *Screener) launch(sc store.Screen) {
	base := s.Base
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithCancel(base)
	if s.running == nil {
		s.running = map[string]context.CancelFunc{}
	}
	s.running[sc.ID] = cancel
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() {
			cancel()
			s.mu.Lock()
			delete(s.running, sc.ID)
			s.mu.Unlock()
		}()
		s.run(ctx, sc)
	}()
}

// run takes a screen through both stages, skipping work already saved.
func (s *Screener) run(ctx context.Context, sc store.Screen) {
	log := s.logger().With("screen", sc.ID)
	// Status changes must land even when ctx was just cancelled. A screen
	// stopped by shutdown keeps its status, so the next start resumes it.
	finish := func(status, errorText string) {
		if status == "cancelled" && s.Base != nil && s.Base.Err() != nil {
			log.Info("screen paused by shutdown")
			return
		}
		if err := s.Store.SetStatus(context.WithoutCancel(ctx), sc.ID, status, errorText); err != nil {
			log.Error("screen status", "err", err)
		}
		log.Info("screen finished", "status", status)
	}

	saved, err := s.Store.Results(ctx, sc.ID)
	if err != nil {
		finish("failed", err.Error())
		return
	}
	done := map[string]bool{}
	for _, r := range saved {
		done[r.Ticker] = true
	}

	// Stage 1: fundamentals for every company.
	if sc.Status == "scanning" {
		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(max(s.Workers, 1))
		for _, m := range sc.Members {
			if done[m.Ticker] {
				continue
			}
			g.Go(func() error {
				if gctx.Err() != nil {
					return gctx.Err()
				}
				r := s.fundamentals(gctx, m)
				if gctx.Err() != nil {
					return gctx.Err() // cancelled mid-fetch: not a real failure
				}
				return s.Store.SaveResult(gctx, sc.ID, r)
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
		if err := s.queuePrices(ctx, sc); err != nil {
			finish("failed", err.Error())
			return
		}
		if err := s.Store.SetStatus(ctx, sc.ID, "pricing", ""); err != nil {
			finish("failed", err.Error())
			return
		}
	}

	// Stage 2: prices for the queued companies, one at a time (the price
	// feed's limiter paces them).
	results, err := s.Store.Results(ctx, sc.ID)
	if err != nil {
		finish("failed", err.Error())
		return
	}
	sortByBusiness(results)
	for _, r := range results {
		if r.Stage != StageQueued {
			continue
		}
		priced := s.price(ctx, r)
		if ctx.Err() != nil {
			finish("cancelled", "")
			return
		}
		if err := s.Store.SaveResult(ctx, sc.ID, priced); err != nil {
			finish("failed", err.Error())
			return
		}
	}
	finish("done", "")
}

// queuePrices picks the companies whose prices are looked up: business
// score at least MinBusiness, strongest first, at most MaxPriced.
func (s *Screener) queuePrices(ctx context.Context, sc store.Screen) error {
	results, err := s.Store.Results(ctx, sc.ID)
	if err != nil {
		return err
	}
	sortByBusiness(results)
	queued := 0 // including any queued before a restart
	for _, r := range results {
		if r.Stage == StageQueued || r.Stage == StagePriced {
			queued++
		}
	}
	for _, r := range results {
		if r.Stage != StageScanned {
			continue
		}
		switch {
		case r.Business == nil || *r.Business < sc.Params.MinBusiness:
			r.Stage = StageBelow
		case queued >= sc.Params.MaxPriced:
			r.Stage = StageCapped
		default:
			r.Stage = StageQueued
			queued++
		}
		if err := s.Store.SaveResult(ctx, sc.ID, r); err != nil {
			return err
		}
	}
	return nil
}

// fundamentals reads one company's filings and scores its business.
func (s *Screener) fundamentals(ctx context.Context, m store.ScreenMember) store.ScreenResult {
	r := store.ScreenResult{Ticker: m.Ticker, Name: m.Name, Sector: m.Sector, Stage: StageScanned}
	b, err := s.Fundamentals.Fetch(ctx, m.Ticker)
	if err != nil {
		r.Stage, r.Error = StageFailed, err.Error()
		return r
	}
	if r.Name == "" {
		r.Name = b.Company.Name
	}
	s.fill(&r, metrics.Compute(b, s.Config))
	return r
}

// price adds the valuation to a queued result.
func (s *Screener) price(ctx context.Context, r store.ScreenResult) store.ScreenResult {
	b, err := s.Priced.Fetch(ctx, r.Ticker)
	if err != nil {
		r.Stage, r.Error = StageFailed, err.Error()
		return r
	}
	rep := metrics.Compute(b, s.Config)
	s.fill(&r, rep)
	r.Stage = StagePriced
	r.Valuation = rep.ValuationScore.Value
	r.Price, r.PriceDate, r.MarketCap = rep.Price, rep.PriceDate, rep.MarketCap.Value
	if bands := rep.Bands; bands != nil {
		r.Metric, r.Zone = bands.Metric, bands.Zone
		r.Percentile, r.Current = &bands.Percentile, &bands.Current
		r.CheapPrice, r.MedianPrice, r.ExpensivePrice = &bands.CheapPrice, &bands.MedianPrice, &bands.ExpensivePrice
	}
	if d := rep.ReverseDCF; d != nil {
		r.Expectations = d.Expectations
	}
	if r.Valuation == nil {
		// Why there is no valuation: a price problem, or too little history.
		r.Error = strings.Join(rep.ValuationScore.Reasons, "; ")
		for _, w := range b.Warnings {
			if strings.Contains(strings.ToLower(w), "price") {
				r.Error = w
			}
		}
	}
	return r
}

// fill copies what the stages share from a computed report.
func (s *Screener) fill(r *store.ScreenResult, rep metrics.Report) {
	r.Limited = rep.LimitedSupport != nil
	if rep.Sector != nil && !r.Limited {
		r.Kind = rep.Sector.Kind
	}
	r.Phase = rep.Phase.Name
	r.Growth, r.Health = rep.GrowthScore.Value, rep.HealthScore.Value
	r.Business = Business(s.Config.Weights, r.Growth, r.Health)
}

// Business is the business score without the AI: the growth and financial
// health (or strength) scores, weighted as in the verdict's composite B and
// renormalized over the ones available. The full report adds the AI's
// business quality, moat and risk.
func Business(w scoring.Weights, growth, health *float64) *float64 {
	var sum, weight float64
	if growth != nil {
		sum += *growth * w.Growth
		weight += w.Growth
	}
	if health != nil {
		sum += *health * w.FinancialHealth
		weight += w.FinancialHealth
	}
	if weight == 0 {
		return nil
	}
	b := float64(int(sum/weight*100+0.5)) / 100
	return &b
}

func sortByBusiness(rs []store.ScreenResult) {
	score := func(r store.ScreenResult) float64 {
		if r.Business == nil {
			return -1
		}
		return *r.Business
	}
	sort.SliceStable(rs, func(i, j int) bool {
		if score(rs[i]) != score(rs[j]) {
			return score(rs[i]) > score(rs[j])
		}
		return rs[i].Ticker < rs[j].Ticker
	})
}

func (s *Screener) logger() *slog.Logger {
	if s.Logger == nil {
		return slog.New(slog.DiscardHandler)
	}
	return s.Logger
}
