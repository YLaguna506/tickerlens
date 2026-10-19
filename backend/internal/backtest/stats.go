package backtest

import (
	"fmt"
	"math"
	"sort"

	"github.com/YLaguna506/tickerlens/backend/internal/store"
)

// Bucket is a group of observations (one verdict, one V score, …) and how
// they did. Excess is the return above the benchmark; the All bucket is the
// baseline to compare a group with, since today's index members did better
// than the benchmark on average (survivorship).
type Bucket struct {
	Key          string      `json:"key"`
	Count        [3]int      `json:"count"` // observations with a return, per horizon
	MeanReturn   [3]*float64 `json:"meanReturn"`
	MeanExcess   [3]*float64 `json:"meanExcess"`
	MedianExcess [3]*float64 `json:"medianExcess"`
	// HitRate is the share of observations that beat the benchmark.
	HitRate [3]*float64 `json:"hitRate"`
}

// Signal is how well one input ranks stocks by what they did next: the
// Spearman rank correlation (information coefficient) between the input and
// the excess return, computed date by date and averaged.
type Signal struct {
	Input string      `json:"input"`
	IC    [3]*float64 `json:"ic"`
	// T is the mean IC over its standard error across dates. Windows of
	// two and three years overlap from one date to the next, which
	// overstates it for those horizons.
	T       [3]*float64 `json:"t"`
	Dates   [3]int      `json:"dates"`
	Verdict [3]string   `json:"verdict"` // helps | hurts | no clear signal | too little data
}

// Summary is the backtest's answer.
type Summary struct {
	Observations int      `json:"observations"`
	Companies    int      `json:"companies"`
	Dates        []string `json:"dates"`
	All          Bucket   `json:"all"`
	ByLabel      []Bucket `json:"byLabel"`
	ByValuation  []Bucket `json:"byValuation"`
	ByBusiness   []Bucket `json:"byBusiness"`
	ByZone       []Bucket `json:"byZone"`
	ByGrowth     []Bucket `json:"byGrowth"`
	ByHealth     []Bucket `json:"byHealth"`
	Signals      []Signal `json:"signals"`
}

// minPerDate is the fewest observations a date needs for its correlation.
const minPerDate = 10

// Summarize groups the observations and measures each input's signal.
func Summarize(obs []store.Observation) Summary {
	s := Summary{Observations: len(obs), All: bucket("all", obs)}
	companies, dates := map[string]bool{}, map[string]bool{}
	for _, o := range obs {
		companies[o.Ticker] = true
		dates[o.Date] = true
	}
	s.Companies = len(companies)
	for d := range dates {
		s.Dates = append(s.Dates, d)
	}
	sort.Strings(s.Dates)

	s.ByLabel = groups(obs, []string{"Buy", "Watch", "Hold", "Pass", "Sell"}, func(o store.Observation) string { return o.Label })
	s.ByValuation = groups(obs, []string{"5", "4", "3", "2", "1"}, func(o store.Observation) string { return whole(o.Valuation) })
	s.ByGrowth = groups(obs, []string{"5", "4", "3", "2", "1"}, func(o store.Observation) string { return whole(o.Growth) })
	s.ByZone = groups(obs, []string{"cheap", "fair", "expensive"}, func(o store.Observation) string { return o.Zone })
	bands := []string{"4.5–5", "4–4.5", "3.5–4", "3–3.5", "2.5–3", "1–2.5"}
	s.ByBusiness = groups(obs, bands, func(o store.Observation) string { return band(o.Business) })
	s.ByHealth = groups(obs, bands, func(o store.Observation) string { return band(o.Health) })

	for _, in := range []struct {
		name string
		get  func(store.Observation) *float64
	}{
		{"business", func(o store.Observation) *float64 { return o.Business }},
		{"growth", func(o store.Observation) *float64 { return o.Growth }},
		{"health", func(o store.Observation) *float64 { return o.Health }},
		{"valuation", func(o store.Observation) *float64 { return o.Valuation }},
		// Cheapness against its own history, before V's steps and the
		// reverse DCF adjustment: 1 − percentile.
		{"cheapness", func(o store.Observation) *float64 {
			if o.Percentile == nil {
				return nil
			}
			c := 1 - *o.Percentile
			return &c
		}},
	} {
		s.Signals = append(s.Signals, signal(in.name, obs, in.get))
	}
	return s
}

func groups(obs []store.Observation, keys []string, key func(store.Observation) string) []Bucket {
	by := map[string][]store.Observation{}
	for _, o := range obs {
		by[key(o)] = append(by[key(o)], o)
	}
	var out []Bucket
	for _, k := range keys {
		if len(by[k]) > 0 {
			out = append(out, bucket(k, by[k]))
		}
	}
	return out
}

func bucket(key string, obs []store.Observation) Bucket {
	b := Bucket{Key: key}
	for h := range Horizons {
		var rets, excess []float64
		beat := 0
		for _, o := range obs {
			if o.Return[h] != nil {
				rets = append(rets, *o.Return[h])
			}
			if o.Excess[h] != nil {
				excess = append(excess, *o.Excess[h])
				if *o.Excess[h] > 0 {
					beat++
				}
			}
		}
		b.Count[h] = len(excess)
		if len(rets) > 0 {
			b.MeanReturn[h] = ptr(mean(rets))
		}
		if len(excess) > 0 {
			b.MeanExcess[h] = ptr(mean(excess))
			b.MedianExcess[h] = ptr(median(excess))
			b.HitRate[h] = ptr(float64(beat) / float64(len(excess)))
		}
	}
	return b
}

func signal(name string, obs []store.Observation, get func(store.Observation) *float64) Signal {
	s := Signal{Input: name}
	byDate := map[string][]store.Observation{}
	for _, o := range obs {
		byDate[o.Date] = append(byDate[o.Date], o)
	}
	for h := range Horizons {
		var ics []float64
		for _, list := range byDate {
			var xs, ys []float64
			for _, o := range list {
				if x := get(o); x != nil && o.Excess[h] != nil {
					xs = append(xs, *x)
					ys = append(ys, *o.Excess[h])
				}
			}
			if len(xs) < minPerDate {
				continue
			}
			if ic, ok := spearman(xs, ys); ok {
				ics = append(ics, ic)
			}
		}
		s.Dates[h] = len(ics)
		if len(ics) < 3 {
			s.Verdict[h] = "too little data"
			continue
		}
		m, sd := mean(ics), stddev(ics)
		s.IC[h] = ptr(m)
		if sd > 0 {
			s.T[h] = ptr(m / (sd / math.Sqrt(float64(len(ics)))))
		}
		// Without spread (the same correlation every date), the sign decides.
		consistent := sd == 0 && m != 0
		switch {
		case m > 0 && (consistent || (s.T[h] != nil && *s.T[h] >= 2)):
			s.Verdict[h] = "helps"
		case m < 0 && (consistent || (s.T[h] != nil && *s.T[h] <= -2)):
			s.Verdict[h] = "hurts"
		default:
			s.Verdict[h] = "no clear signal"
		}
	}
	return s
}

// spearman is the rank correlation of xs and ys (ties share their average
// rank). It is undefined when either has no spread.
func spearman(xs, ys []float64) (float64, bool) {
	rx, ry := ranks(xs), ranks(ys)
	mx, my := mean(rx), mean(ry)
	var num, dx, dy float64
	for i := range rx {
		num += (rx[i] - mx) * (ry[i] - my)
		dx += (rx[i] - mx) * (rx[i] - mx)
		dy += (ry[i] - my) * (ry[i] - my)
	}
	if dx == 0 || dy == 0 {
		return 0, false
	}
	return num / math.Sqrt(dx*dy), true
}

func ranks(v []float64) []float64 {
	idx := make([]int, len(v))
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool { return v[idx[a]] < v[idx[b]] })
	out := make([]float64, len(v))
	for i := 0; i < len(idx); {
		j := i
		for j+1 < len(idx) && v[idx[j+1]] == v[idx[i]] {
			j++
		}
		r := float64(i+j)/2 + 1
		for k := i; k <= j; k++ {
			out[idx[k]] = r
		}
		i = j + 1
	}
	return out
}

func mean(v []float64) float64 {
	sum := 0.0
	for _, x := range v {
		sum += x
	}
	return sum / float64(len(v))
}

func median(v []float64) float64 {
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

func stddev(v []float64) float64 {
	if len(v) < 2 {
		return 0
	}
	m := mean(v)
	ss := 0.0
	for _, x := range v {
		ss += (x - m) * (x - m)
	}
	return math.Sqrt(ss / float64(len(v)-1))
}

// whole rounds a 1–5 score to its whole step ("4").
func whole(v *float64) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%.0f", math.Round(*v))
}

// band places a 1–5 score in half-point bands.
func band(v *float64) string {
	switch {
	case v == nil:
		return ""
	case *v >= 4.5:
		return "4.5–5"
	case *v >= 4:
		return "4–4.5"
	case *v >= 3.5:
		return "3.5–4"
	case *v >= 3:
		return "3–3.5"
	case *v >= 2.5:
		return "2.5–3"
	default:
		return "1–2.5"
	}
}

func ptr(v float64) *float64 { return &v }
