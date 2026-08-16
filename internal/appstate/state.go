package appstate

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type State struct {
	Fund   *FundState
	Prices *PriceState
	FX     *FXState
}

type FXState struct {
	Rates map[FXPair]FXRate
}

type FXPair struct {
	BaseCurrency  string
	QuoteCurrency string
}

type FXRate struct {
	BaseCurrency  string
	QuoteCurrency string
	Provider      string
	Rate          string
	PricedAt      time.Time
	FetchedAt     time.Time
}

type PriceState struct {
	Sources     map[int64]InstrumentPrice
	DailyPrices map[int64]InstrumentDailyPriceSeries
	Points      map[int64]InstrumentPricePointSeries
}

type InstrumentDailyPriceSeries struct {
	PriceSourceID int64
	InstrumentID  int64

	AssetType string
	ISIN      string
	Name      string

	Provider       string
	ProviderSymbol string

	Items []InstrumentDailyPrice
}

type InstrumentDailyPrice struct {
	PriceDate time.Time
	UnitValue string
	Currency  string
}

type InstrumentPricePointSeries struct {
	PriceSourceID int64
	InstrumentID  int64

	AssetType string
	ISIN      string
	Name      string

	Provider       string
	ProviderSymbol string

	Items []InstrumentPricePoint
}

type InstrumentPricePoint struct {
	UnitValue  string
	Currency   string
	PricedAt   time.Time
	ObservedAt time.Time
}

type InstrumentPrice struct {
	PriceSourceID int64
	InstrumentID  int64

	AssetType string
	ISIN      string
	Name      string

	Provider       string
	ProviderSymbol string

	UnitValue string
	Currency  string

	PricedAt  time.Time
	FetchedAt time.Time
}

type FundState struct {
	Snapshot    FundSnapshot
	DailyValues []FundDailyValue
}

type FundSnapshot struct {
	ID         int64
	AsOfDate   time.Time
	ObservedAt time.Time
	SourceHash string

	CalculatedUnitValueUSD string
	NAVUSD                 string

	Assets     []FundAsset
	Categories []FundCategory
}

type FundAsset struct {
	ID    int64
	RowNo int

	SourceName string
	SourceType string

	InstrumentID   *int64
	InstrumentType string
	ISIN           string
	InstrumentName string
	Issuer         string
	Ticker         string

	Currency string
	Quantity string

	AssetSharePercent    string
	AssetShareUpperBound bool
}

type FundCategory struct {
	RowNo int

	SourceName        string
	AssetSharePercent string
}

type FundDailyValue struct {
	AsOfDate time.Time

	CalculatedUnitValueUSD string
	NAVUSD                 string
}

type Manager struct {
	current atomic.Pointer[State]
	writeMu sync.Mutex
}

func NewManager() *Manager {
	return &Manager{}
}

func (m *Manager) Load() *State {
	return m.current.Load()
}

func (m *Manager) Initialize(state *State) error {
	if state == nil {
		return fmt.Errorf("application state is nil")
	}

	m.writeMu.Lock()
	defer m.writeMu.Unlock()

	if m.current.Load() != nil {
		return fmt.Errorf("application state is already initialized")
	}

	m.current.Store(state)
	return nil
}

func (m *Manager) Update(update func(current *State) (*State, error)) error {
	if update == nil {
		return fmt.Errorf("application state update func is nil")
	}

	m.writeMu.Lock()
	defer m.writeMu.Unlock()

	current := m.current.Load()
	next, err := update(current)
	if err != nil {
		return err
	}
	if next == nil {
		return fmt.Errorf("application state update returned nil state")
	}

	if next != current {
		m.current.Store(next)
	}

	return nil
}
