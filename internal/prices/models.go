package prices

import (
	"context"
	"time"

	"github.com/rufond/fpr-backend/internal/appstate"
)

const (
	ProviderMOEX  = "moex"
	ProviderYahoo = "yahoo"

	FundUnitAssetType = "fund_unit"
	FundUnitISIN      = "RU000A101NK4"
	FundUnitName      = "Фонд первичных размещений"
)

type Source interface {
	FetchFundUnitQuote(ctx context.Context) (*SourceQuote, error)
	FetchFundUnitDailyPrices(ctx context.Context, from time.Time) ([]SourceDailyPrice, error)
}

type YahooSource interface {
	FetchPrices(ctx context.Context, symbols []string) (*YahooSourceResult, error)
}

type SourceQuote struct {
	UnitValue string
	Currency  string
	PricedAt  time.Time
	Source    string
}

type SourceDailyPrice struct {
	PriceDate time.Time
	UnitValue string
	Currency  string
}

type SyncResult struct {
	Changed bool
	Stale   bool
	Source  string
	Price   appstate.InstrumentPrice
}

type DailySyncResult struct {
	Inserted int
	Updated  int
	FromDate time.Time
	ToDate   time.Time
}

func (r DailySyncResult) Changed() bool {
	return r.Inserted != 0 || r.Updated != 0
}

type YahooSourceQuote struct {
	UnitValue string
	Currency  string
	PricedAt  time.Time
}

type YahooQuoteIssue struct {
	Symbol string
	Error  string
}

type YahooSourceResult struct {
	RequestedSymbols int
	ReturnedSymbols  int
	Batches          int

	QuotesByRequest map[string]YahooSourceQuote
	MissingRequests int
	InvalidRequests int

	Missing    []string
	Unexpected []string
	Duplicates []string
	Invalid    []YahooQuoteIssue
}

type YahooSyncResult struct {
	ExpectedSources  int
	RequestedSymbols int
	ReturnedSymbols  int
	Batches          int

	ChangedPrices []appstate.InstrumentPrice

	UnchangedSources          int
	StaleSources              int
	MissingSources            int
	InvalidSources            int
	CompositionSkippedSources int

	MissingSymbols    []string
	UnexpectedSymbols []string
	DuplicateSymbols  []string
	InvalidQuotes     []YahooQuoteIssue
}

func (r YahooSyncResult) Changed() bool {
	return len(r.ChangedPrices) != 0
}

type yahooPriceSource struct {
	PriceSourceID  int64  `db:"price_source_id"`
	InstrumentID   int64  `db:"instrument_id"`
	ProviderSymbol string `db:"provider_symbol"`
}

type yahooQuoteToApply struct {
	PriceSourceID int64
	InstrumentID  int64
	Quote         SourceQuote
}

type yahooApplyResult struct {
	ChangedPrices []appstate.InstrumentPrice
	Unchanged     int
	Stale         int
}
