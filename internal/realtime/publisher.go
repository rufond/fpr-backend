package realtime

const (
	ScopeDiagnostics = "diagnostics"
	ScopeFundHistory = "fund_history"
	ScopeFundState   = "fund_state"
	ScopeScheduler   = "scheduler"
)

type Update struct {
	Scopes        []string
	InstrumentIDs []int64
}

type Publisher interface {
	Publish(update Update)
}

type DiscardPublisher struct{}

func (DiscardPublisher) Publish(Update) {}
