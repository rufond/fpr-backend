package diagnostics

type Issue struct {
	Key          string         `json:"key"`
	Severity     string         `json:"severity"`
	Source       string         `json:"source"`
	Type         string         `json:"type"`
	Message      string         `json:"message"`
	InstrumentID *int64         `json:"instrument_id"`
	Details      map[string]any `json:"details"`
}

type Result struct {
	Total    int     `json:"total"`
	Errors   int     `json:"errors"`
	Warnings int     `json:"warnings"`
	Items    []Issue `json:"items"`
}
