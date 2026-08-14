package realtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"slices"
	"sort"
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
	Type          string    `json:"type"`
	GenerationID  string    `json:"generation_id"`
	Revision      uint64    `json:"revision"`
	OccurredAt    time.Time `json:"occurred_at"`
	Scopes        []string  `json:"scopes,omitempty"`
	InstrumentIDs []int64   `json:"instrument_ids,omitempty"`
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
		generationID: newGenerationID(),
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

	instrumentIDs := normalizeInstrumentIDs(update.InstrumentIDs)

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return
	}

	h.revision++

	event := Event{
		Type:          eventTypeChanged,
		GenerationID:  h.generationID,
		Revision:      h.revision,
		OccurredAt:    time.Now().UTC(),
		Scopes:        scopes,
		InstrumentIDs: instrumentIDs,
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
	result := slices.Clone(scopes)
	sort.Strings(result)
	result = slices.Compact(result)

	filtered := result[:0]
	for _, scope := range result {
		if scope != "" {
			filtered = append(filtered, scope)
		}
	}

	return filtered
}

func normalizeInstrumentIDs(ids []int64) []int64 {
	result := slices.Clone(ids)
	slices.Sort(result)
	result = slices.Compact(result)

	filtered := result[:0]
	for _, id := range result {
		if id > 0 {
			filtered = append(filtered, id)
		}
	}

	return filtered
}

func newGenerationID() string {
	var value [16]byte

	if _, err := rand.Read(value[:]); err == nil {
		return hex.EncodeToString(value[:])
	}

	return time.Now().UTC().Format("20060102T150405.000000000")
}
