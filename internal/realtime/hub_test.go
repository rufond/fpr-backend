package realtime

import (
	"context"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestHubPublishUsesCanonicalInternalData(t *testing.T) {
	hub := NewHub()
	subscriber, ok := hub.register()
	if !ok {
		t.Fatal("register returned false")
	}
	defer hub.unregister(subscriber)

	<-subscriber.events

	pricedAt := time.Date(2026, time.August, 14, 13, 42, 31, 0, time.UTC)
	hub.Publish(Update{
		Scopes: []string{ScopeFundState, ScopeFXRates, ScopeInstrumentPrices},
		InstrumentPrices: []InstrumentPriceDelta{
			{InstrumentID: 9, UnitValue: "3210.5", Currency: "RUB", PricedAt: pricedAt},
		},
		FXRates: []FXRateDelta{
			{BaseCurrency: "USD", QuoteCurrency: "RUB", Rate: "79.125", PricedAt: pricedAt},
		},
	})

	event := <-subscriber.events

	if event.Type != eventTypeChanged || event.Revision != 1 {
		t.Fatalf("event = %#v", event)
	}
	if !slices.Equal(event.Scopes, []string{ScopeFundState, ScopeFXRates, ScopeInstrumentPrices}) {
		t.Fatalf("scopes = %#v", event.Scopes)
	}
	if !slices.Equal(event.InstrumentIDs, []int64{9}) {
		t.Fatalf("instrument ids = %#v", event.InstrumentIDs)
	}
	if len(event.InstrumentPrices) != 1 || event.InstrumentPrices[0].InstrumentID != 9 {
		t.Fatalf("instrument prices = %#v", event.InstrumentPrices)
	}
	if len(event.FXRates) != 1 || event.FXRates[0].Rate != "79.125" {
		t.Fatalf("FX rates = %#v", event.FXRates)
	}
}

func TestHubPublishIgnoresNoScopes(t *testing.T) {
	hub := NewHub()
	subscriber, ok := hub.register()
	if !ok {
		t.Fatal("register returned false")
	}
	defer hub.unregister(subscriber)

	<-subscriber.events
	hub.Publish(Update{})

	select {
	case event := <-subscriber.events:
		t.Fatalf("unexpected event = %#v", event)
	default:
	}

	if hub.revision != 0 {
		t.Fatalf("revision = %d, want 0", hub.revision)
	}
}

func TestHubCloseClosesSubscribers(t *testing.T) {
	hub := NewHub()
	subscriber, ok := hub.register()
	if !ok {
		t.Fatal("register returned false")
	}

	<-subscriber.events
	hub.Close()

	if _, open := <-subscriber.events; open {
		t.Fatal("subscriber channel remained open")
	}

	if _, ok := hub.register(); ok {
		t.Fatal("register succeeded after close")
	}
}

func TestHubServeHTTPStreamsEvents(t *testing.T) {
	hub := NewHub()
	server := httptest.NewServer(hub)
	defer server.Close()
	defer hub.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	connection, _, errDial := websocket.Dial(ctx, strings.Replace(server.URL, "http://", "ws://", 1), nil)
	if errDial != nil {
		t.Fatalf("websocket.Dial() error = %v", errDial)
	}
	defer func(connection *websocket.Conn) {
		_ = connection.CloseNow()
	}(connection)

	var hello Event
	if errRead := wsjson.Read(ctx, connection, &hello); errRead != nil {
		t.Fatalf("read hello: %v", errRead)
	}

	if hello.Type != eventTypeHello {
		t.Fatalf("hello type = %q, want %q", hello.Type, eventTypeHello)
	}

	hub.Publish(Update{Scopes: []string{ScopeFundState}})

	var event Event
	if errRead := wsjson.Read(ctx, connection, &event); errRead != nil {
		t.Fatalf("read changed event: %v", errRead)
	}

	if event.Type != eventTypeChanged || event.Revision != 1 {
		t.Fatalf("event = %#v", event)
	}

	if !slices.Equal(event.Scopes, []string{ScopeFundState}) {
		t.Fatalf("event payload = %#v", event)
	}
}
