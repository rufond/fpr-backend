package prices

import (
	"context"
	"time"

	"github.com/rufond/fpr-backend/internal/appstate"
)

const (
	ProviderKASE  = "kase"
	ProviderMOEX  = "moex"
	ProviderYahoo = "yahoo"

	FundUnitAssetType = "fund_unit"
	FundUnitISIN      = "RU000A101NK4"
	FundUnitName      = "Фонд первичных размещений"
)

type Source interface {
	FetchFundUnitQuote(ctx context.Context) (SourceQuote, error)
	FetchFundUnitDailyPrices(ctx context.Context, from time.Time) ([]SourceDailyPrice, error)
	ResolveSecuritySymbols(ctx context.Context, isins []string) (MOEXSymbolResolutionResult, error)
	FetchSecurityPrices(ctx context.Context, symbols []string) (MOEXSourceResult, error)
}

type YahooSource interface {
	FetchPrices(ctx context.Context, symbols []string) (YahooSourceResult, error)
	ResolveSymbols(ctx context.Context, isins []string) (YahooSymbolResolutionResult, error)
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

type YahooSymbolResolutionResult struct {
	RequestedISINs int
	SymbolsByISIN  map[string]string
	MissingISINs   []string
}

type YahooSourceDiscoveryResult struct {
	CandidateInstruments int
	ExistingSources      int
	RequestedISINs       int
	ResolvedISINs        int
	CreatedSources       int
	MissingISINs         []string
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

type MOEXSymbolResolutionResult struct {
	RequestedISINs int
	SymbolsByISIN  map[string]string
	MissingISINs   []string
}

type MOEXSourceResult struct {
	RequestedSymbols int
	QuotesBySymbol   map[string]SourceQuote
	Issues           []MOEXQuoteIssue
}

type MOEXQuoteIssue struct {
	Symbol string
	Error  string
}

type MOEXSourceDiscoveryResult struct {
	CandidateInstruments int
	ExistingSources      int
	RequestedISINs       int
	ResolvedISINs        int
	CreatedSources       int
	MissingISINs         []string
}

type MOEXSecuritySyncResult struct {
	ExpectedSources           int
	RequestedSymbols          int
	ChangedPrices             []appstate.InstrumentPrice
	UnchangedSources          int
	StaleSources              int
	FailedSources             int
	CompositionSkippedSources int
	Issues                    []MOEXQuoteIssue
}

func (r MOEXSecuritySyncResult) Changed() bool {
	return len(r.ChangedPrices) != 0
}

type AdminPriceSource struct {
	ID             int64  `json:"id"`
	Provider       string `json:"provider"`
	ProviderSymbol string `json:"provider_symbol"`
	Enabled        bool   `json:"enabled"`
}

type AdminPriceSourceInstrument struct {
	InstrumentID int64              `json:"instrument_id"`
	AssetType    string             `json:"asset_type"`
	ISIN         string             `json:"isin"`
	Name         string             `json:"name"`
	Ticker       string             `json:"ticker,omitempty"`
	Sources      []AdminPriceSource `json:"sources"`
}

type AdminPriceSourcesResult struct {
	Items []AdminPriceSourceInstrument `json:"items"`
}

type SetPriceSourceResult struct {
	Changed      bool             `json:"changed"`
	InstrumentID int64            `json:"instrument_id"`
	Source       AdminPriceSource `json:"source"`
}

type storedPriceSource struct {
	ID             int64  `db:"id"`
	InstrumentID   int64  `db:"instrument_id"`
	Provider       string `db:"provider"`
	ProviderSymbol string `db:"provider_symbol"`
	Enabled        bool   `db:"enabled"`
}

type priceSource struct {
	PriceSourceID  int64  `db:"price_source_id"`
	InstrumentID   int64  `db:"instrument_id"`
	ProviderSymbol string `db:"provider_symbol"`
}

type sourceMapping struct {
	InstrumentID   int64
	ProviderSymbol string
}

type quoteToApply struct {
	PriceSourceID int64
	InstrumentID  int64
	Quote         SourceQuote
}

type applyQuotesResult struct {
	ChangedPrices []appstate.InstrumentPrice
	Unchanged     int
	Stale         int
}
