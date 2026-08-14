package realtime

const (
	ScopeDiagnostics = "diagnostics"
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
