package realtime

import (
	"cmp"
	"context"
	"crypto/rand"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/rs/zerolog/log"
)

const (
	eventTypeChanged = "changed"
	eventTypeHello   = "hello"

	clientBufferSize = 32
	pingInterval     = 30 * time.Second
	writeTimeout     = 10 * time.Second
)

type Event struct {
	Type             string                 `json:"type"`
	GenerationID     string                 `json:"generation_id"`
	Revision         uint64                 `json:"revision"`
	OccurredAt       time.Time              `json:"occurred_at"`
	Scopes           []string               `json:"scopes,omitempty"`
	InstrumentIDs    []int64                `json:"instrument_ids,omitempty"`
	InstrumentPrices []InstrumentPriceDelta `json:"instrument_prices,omitempty"`
	FXRates          []FXRateDelta          `json:"fx_rates,omitempty"`
}

type client struct {
	events chan Event
}

type Hub struct {
	generationID string

	mu       sync.Mutex
	revision uint64
	clients  map[*client]struct{}
	closed   bool
}

func NewHub() *Hub {
	return &Hub{
		generationID: rand.Text(),
		clients:      map[*client]struct{}{},
	}
}

func (h *Hub) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	connection, errAccept := websocket.Accept(writer, request, nil)
	if errAccept != nil {
		log.Warn().Err(errAccept).Str("path", request.URL.Path).Msg("accept realtime websocket")
		return
	}
	defer func(connection *websocket.Conn) {
		_ = connection.CloseNow()
	}(connection)

	connection.SetReadLimit(1024)

	subscriber, ok := h.register()
	if !ok {
		_ = connection.Close(websocket.StatusGoingAway, "realtime hub is stopping")
		return
	}
	defer h.unregister(subscriber)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	readCtx := connection.CloseRead(ctx)
	pingTicker := time.NewTicker(pingInterval)
	defer pingTicker.Stop()

	log.Debug().Str("generation_id", h.generationID).Msg("realtime websocket connected")
	defer log.Debug().Str("generation_id", h.generationID).Msg("realtime websocket disconnected")

	for {
		select {
		case event, open := <-subscriber.events:
			if !open {
				_ = connection.Close(websocket.StatusGoingAway, "realtime hub is stopping")
				return
			}

			writeCtx, writeCancel := context.WithTimeout(ctx, writeTimeout)
			errWrite := wsjson.Write(writeCtx, connection, event)
			writeCancel()

			if errWrite != nil {
				return
			}

		case <-pingTicker.C:
			pingCtx, pingCancel := context.WithTimeout(ctx, writeTimeout)
			errPing := connection.Ping(pingCtx)
			pingCancel()

			if errPing != nil {
				return
			}

		case <-readCtx.Done():
			return
		}
	}
}

func (h *Hub) Publish(update Update) {
	scopes := normalizeScopes(update.Scopes)
	if len(scopes) == 0 {
		return
	}

	instrumentPrices := normalizeInstrumentPrices(update.InstrumentPrices)
	fxRates := normalizeFXRates(update.FXRates)
	instrumentIDs := make([]int64, 0, len(update.InstrumentIDs)+len(instrumentPrices))
	instrumentIDs = append(instrumentIDs, update.InstrumentIDs...)
	for _, price := range instrumentPrices {
		instrumentIDs = append(instrumentIDs, price.InstrumentID)
	}
	instrumentIDs = normalizeInstrumentIDs(instrumentIDs)

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return
	}

	h.revision++

	event := Event{
		Type:             eventTypeChanged,
		GenerationID:     h.generationID,
		Revision:         h.revision,
		OccurredAt:       time.Now().UTC(),
		Scopes:           scopes,
		InstrumentIDs:    instrumentIDs,
		InstrumentPrices: instrumentPrices,
		FXRates:          fxRates,
	}

	for subscriber := range h.clients {
		select {
		case subscriber.events <- event:
		default:
			delete(h.clients, subscriber)
			close(subscriber.events)
		}
	}
}

func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return
	}

	h.closed = true

	for subscriber := range h.clients {
		delete(h.clients, subscriber)
		close(subscriber.events)
	}
}

func (h *Hub) register() (*client, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return nil, false
	}

	subscriber := &client{events: make(chan Event, clientBufferSize)}
	h.clients[subscriber] = struct{}{}

	subscriber.events <- Event{
		Type:         eventTypeHello,
		GenerationID: h.generationID,
		Revision:     h.revision,
		OccurredAt:   time.Now().UTC(),
	}

	return subscriber, true
}

func (h *Hub) unregister(subscriber *client) {
	if subscriber == nil {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.clients[subscriber]; !ok {
		return
	}

	delete(h.clients, subscriber)
	close(subscriber.events)
}

func normalizeScopes(scopes []string) []string {
	result := slices.DeleteFunc(slices.Clone(scopes), func(scope string) bool {
		return scope == ""
	})
	slices.Sort(result)
	return slices.Compact(result)
}

func normalizeInstrumentIDs(ids []int64) []int64 {
	result := slices.DeleteFunc(slices.Clone(ids), func(id int64) bool {
		return id <= 0
	})
	slices.Sort(result)
	return slices.Compact(result)
}

func normalizeInstrumentPrices(prices []InstrumentPriceDelta) []InstrumentPriceDelta {
	byInstrument := make(map[int64]InstrumentPriceDelta, len(prices))
	for _, price := range prices {
		if price.InstrumentID <= 0 {
			continue
		}

		price.PricedAt = price.PricedAt.UTC()
		byInstrument[price.InstrumentID] = price
	}

	result := make([]InstrumentPriceDelta, 0, len(byInstrument))
	for _, price := range byInstrument {
		result = append(result, price)
	}

	slices.SortFunc(result, func(left, right InstrumentPriceDelta) int {
		return cmp.Compare(left.InstrumentID, right.InstrumentID)
	})

	return result
}

func normalizeFXRates(rates []FXRateDelta) []FXRateDelta {
	type pair struct {
		base  string
		quote string
	}

	byPair := make(map[pair]FXRateDelta, len(rates))
	for _, rate := range rates {
		rate.BaseCurrency = strings.ToUpper(strings.TrimSpace(rate.BaseCurrency))
		rate.QuoteCurrency = strings.ToUpper(strings.TrimSpace(rate.QuoteCurrency))
		rate.Rate = strings.TrimSpace(rate.Rate)
		if rate.BaseCurrency == "" || rate.QuoteCurrency == "" || rate.Rate == "" {
			continue
		}

		rate.PricedAt = rate.PricedAt.UTC()
		byPair[pair{base: rate.BaseCurrency, quote: rate.QuoteCurrency}] = rate
	}

	result := make([]FXRateDelta, 0, len(byPair))
	for _, rate := range byPair {
		result = append(result, rate)
	}

	slices.SortFunc(result, func(left, right FXRateDelta) int {
		if byBase := cmp.Compare(left.BaseCurrency, right.BaseCurrency); byBase != 0 {
			return byBase
		}
		return cmp.Compare(left.QuoteCurrency, right.QuoteCurrency)
	})

	return result
}
