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

func TestHubPublishNormalizesEvent(t *testing.T) {
	hub := NewHub()
	subscriber, ok := hub.register()
	if !ok {
		t.Fatal("register returned false")
	}
	defer hub.unregister(subscriber)

	hello := <-subscriber.events
	if hello.Type != eventTypeHello {
		t.Fatalf("hello type = %q, want %q", hello.Type, eventTypeHello)
	}

	hub.Publish(Update{
		Scopes:        []string{ScopeFundState, ScopeFundHistory, ScopeFundState, ""},
		InstrumentIDs: []int64{7, 2, 7, 0, -1},
	})

	event := <-subscriber.events

	if event.Type != eventTypeChanged {
		t.Fatalf("event type = %q, want %q", event.Type, eventTypeChanged)
	}

	if event.Revision != 1 {
		t.Fatalf("revision = %d, want 1", event.Revision)
	}

	if !slices.Equal(event.Scopes, []string{ScopeFundHistory, ScopeFundState}) {
		t.Fatalf("scopes = %#v", event.Scopes)
	}

	if !slices.Equal(event.InstrumentIDs, []int64{2, 7}) {
		t.Fatalf("instrument ids = %#v", event.InstrumentIDs)
	}
}

func TestHubPublishIgnoresEmptyScopes(t *testing.T) {
	hub := NewHub()
	subscriber, ok := hub.register()
	if !ok {
		t.Fatal("register returned false")
	}
	defer hub.unregister(subscriber)

	<-subscriber.events
	hub.Publish(Update{Scopes: []string{"", ""}})

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
