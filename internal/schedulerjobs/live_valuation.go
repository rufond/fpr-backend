package schedulerjobs

import (
	"context"

	"github.com/rufond/fpr-backend/internal/appstate"
	"github.com/rufond/fpr-backend/internal/realtime"
)

type liveValuationRefresher interface {
	Recalculate(ctx context.Context) (appstate.FundLiveValuation, bool, error)
}

type liveValuationEnricher interface {
	Refresh(ctx context.Context) (appstate.FundLiveValuation, bool, error)
}

func refreshLiveValuation(ctx context.Context, service liveValuationRefresher, publisher realtime.Publisher) (bool, error) {
	live, changed, err := service.Recalculate(ctx)
	if err != nil {
		return false, err
	}

	if !changed {
		return false, nil
	}

	publisher.Publish(realtime.LiveValuationUpdate(live))

	return true, nil
}
