package schedulerjobs

import (
	"context"
	"testing"
	"time"

	"github.com/rufond/fpr-backend/internal/appstate"
	"github.com/rufond/fpr-backend/internal/realtime"
)

type fakeLiveValuationRefresher struct {
	live    appstate.FundLiveValuation
	changed bool
	err     error
}

func (f fakeLiveValuationRefresher) Recalculate(context.Context) (appstate.FundLiveValuation, bool, error) {
	return f.live, f.changed, f.err
}

func (f fakeLiveValuationRefresher) Refresh(context.Context) (appstate.FundLiveValuation, bool, error) {
	return f.live, f.changed, f.err
}

func TestRefreshLiveValuationPublishesDelta(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.August, 17, 18, 30, 0, 0, time.UTC)
	publisher := &captureRealtimePublisher{}
	changed, err := refreshLiveValuation(context.Background(), fakeLiveValuationRefresher{
		changed: true,
		live: appstate.FundLiveValuation{
			ObservedAt:                      observedAt,
			EstimatedNAVUSD:                 "493100000.25",
			EstimatedCalculatedUnitValueUSD: "31.187169",
			LiveDeltaUSD:                    "113350.25",
			LiveCoveragePercent:             "74.5",
		},
	}, publisher)
	if err != nil {
		t.Fatalf("refreshLiveValuation() error = %v", err)
	}
	if !changed {
		t.Fatal("changed = false")
	}
	if len(publisher.updates) != 1 {
		t.Fatalf("updates = %#v", publisher.updates)
	}

	update := publisher.updates[0]
	if len(update.Scopes) != 1 || update.Scopes[0] != realtime.ScopeLiveValuation || update.LiveValuation == nil {
		t.Fatalf("update = %#v", update)
	}
	if update.LiveValuation.EstimatedNAVUSD != "493100000.25" || update.LiveValuation.LiveCoveragePercent != "74.5" {
		t.Fatalf("live valuation = %#v", update.LiveValuation)
	}
}
