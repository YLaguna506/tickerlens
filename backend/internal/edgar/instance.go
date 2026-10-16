package edgar

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/text/encoding/charmap"
)

// DimFact is one value from a filing's XBRL instance, including its
// dimensions (e.g. StatementClassOfStockAxis = CommonClassAMember). The
// companyfacts feed drops every fact that has dimensions; these are them.
type DimFact struct {
	Concept string            `json:"concept"` // local name, e.g. "EarningsPerShareDiluted"
	Start   string            `json:"start,omitempty"`
	End     string            `json:"end"`
	Val     float64           `json:"val"`
	Dims    map[string]string `json:"dims,omitempty"` // axis local name → member local name
}

// xbrlContext is the period and dimensions a fact's contextRef points to.
type xbrlContext struct {
	start, end string
	dims       map[string]string
}

// ParseInstance streams an XBRL instance document and returns the facts for
// the given us-gaap concepts. It reads token by token (encoding/xml.Decoder)
// rather than loading the whole tree, since instances are several MB.
func ParseInstance(r io.Reader, concepts []string) ([]DimFact, error) {
	facts, _, err := parseInstance(r, concepts)
	return facts, err
}

// DocumentInfo is the fiscal period a filing reports on (dei cover facts).
type DocumentInfo struct {
	FiscalYear   int    // DocumentFiscalYearFocus, e.g. 2026
	FiscalPeriod string // DocumentFiscalPeriodFocus: FY, Q1, Q2, Q3
}

// ParseFiling returns the us-gaap facts for concepts plus the filing's
// fiscal year and period.
func ParseFiling(r io.Reader, concepts []string) ([]DimFact, DocumentInfo, error) {
	return parseInstance(r, concepts)
}

func parseInstance(r io.Reader, concepts []string) ([]DimFact, DocumentInfo, error) {
	var info DocumentInfo
	dec := xml.NewDecoder(r)
	dec.CharsetReader = charsetReader
	contexts := map[string]xbrlContext{}
	type rawFact struct {
		concept, contextRef, text string
	}
	var raws []rawFact

	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, info, fmt.Errorf("parse xbrl instance: %w", err)
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch {
		case start.Name.Local == "context" && start.Name.Space == "http://www.xbrl.org/2003/instance":
			var c struct {
				ID     string `xml:"id,attr"`
				Entity struct {
					Segment struct {
						Members []struct {
							Dimension string `xml:"dimension,attr"`
							Value     string `xml:",chardata"`
						} `xml:"explicitMember"`
					} `xml:"segment"`
				} `xml:"entity"`
				Period struct {
					Start   string `xml:"startDate"`
					End     string `xml:"endDate"`
					Instant string `xml:"instant"`
				} `xml:"period"`
			}
			// DecodeElement reads the rest of this element into the struct,
			// mapping child elements by the struct tags (like XmlSerializer).
			if err := dec.DecodeElement(&c, &start); err != nil {
				return nil, info, fmt.Errorf("parse xbrl context: %w", err)
			}
			ctx := xbrlContext{start: c.Period.Start, end: c.Period.End, dims: map[string]string{}}
			if ctx.end == "" {
				ctx.end = c.Period.Instant
			}
			for _, m := range c.Entity.Segment.Members {
				ctx.dims[localName(m.Dimension)] = localName(strings.TrimSpace(m.Value))
			}
			contexts[c.ID] = ctx

		case strings.HasPrefix(start.Name.Space, "http://fasb.org/us-gaap/") && slices.Contains(concepts, start.Name.Local):
			var text string
			if err := dec.DecodeElement(&text, &start); err != nil {
				return nil, info, fmt.Errorf("parse xbrl fact %s: %w", start.Name.Local, err)
			}
			raws = append(raws, rawFact{concept: start.Name.Local, contextRef: attr(start, "contextRef"), text: text})

		case strings.HasPrefix(start.Name.Space, "http://xbrl.sec.gov/dei/") &&
			(start.Name.Local == "DocumentFiscalYearFocus" || start.Name.Local == "DocumentFiscalPeriodFocus"):
			var text string
			if err := dec.DecodeElement(&text, &start); err != nil {
				return nil, info, fmt.Errorf("parse dei %s: %w", start.Name.Local, err)
			}
			text = strings.TrimSpace(text)
			if start.Name.Local == "DocumentFiscalYearFocus" {
				info.FiscalYear, _ = strconv.Atoi(text)
			} else {
				info.FiscalPeriod = text
			}
		}
	}

	// Facts may appear before their contexts, so resolve them at the end.
	// Inline XBRL repeats some facts; dedupe on concept + context.
	seen := map[string]bool{}
	var out []DimFact
	for _, rf := range raws {
		ctx, ok := contexts[rf.contextRef]
		if !ok {
			continue
		}
		val, err := strconv.ParseFloat(strings.TrimSpace(rf.text), 64)
		if err != nil {
			continue // nil values are written as empty elements
		}
		key := rf.concept + "|" + rf.contextRef
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, DimFact{Concept: rf.concept, Start: ctx.start, End: ctx.end, Val: val, Dims: ctx.dims})
	}
	return out, info, nil
}

// charsetReader lets the decoder read older filings that declare a non-UTF-8
// encoding. Go's decoder only understands UTF-8 out of the box.
func charsetReader(label string, input io.Reader) (io.Reader, error) {
	switch strings.ToLower(label) {
	case "us-ascii", "ascii", "utf-8":
		return input, nil // ASCII is a subset of UTF-8
	case "iso-8859-1", "latin1", "windows-1252", "cp1252":
		return charmap.Windows1252.NewDecoder().Reader(input), nil
	default:
		return nil, fmt.Errorf("unsupported XML encoding %q", label)
	}
}

// localName strips a namespace prefix: "us-gaap:CommonClassAMember" → "CommonClassAMember".
func localName(qname string) string {
	if i := strings.LastIndexByte(qname, ':'); i >= 0 {
		return qname[i+1:]
	}
	return qname
}

func attr(e xml.StartElement, name string) string {
	for _, a := range e.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}
