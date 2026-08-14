package scheduler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

type RunWriter struct {
	mu         sync.Mutex
	messages   []map[string]any
	lastFlush  time.Time
	flushEvery time.Duration
	flush      func(messages []map[string]any) error
}

func NewRunWriter(flushEvery time.Duration, flush func(messages []map[string]any) error) *RunWriter {
	return &RunWriter{
		lastFlush:  time.Now(),
		flushEvery: flushEvery,
		flush:      flush,
	}
}

func (w *RunWriter) Write(p []byte) (int, error) {
	line := bytes.TrimSpace(p)
	if len(line) == 0 {
		return len(p), nil
	}

	var message map[string]any
	if err := json.Unmarshal(line, &message); err != nil {
		message = map[string]any{
			"message": string(line),
		}
	}

	message = cleanRunMessage(message)

	w.mu.Lock()
	w.messages = append(w.messages, message)

	shouldFlush := w.flushEvery > 0 &&
		w.flush != nil &&
		time.Since(w.lastFlush) >= w.flushEvery

	var snapshot []map[string]any
	if shouldFlush {
		snapshot = w.copyMessagesLocked()
		w.lastFlush = time.Now()
	}

	w.mu.Unlock()

	if shouldFlush {
		if err := w.flush(snapshot); err != nil {
			return len(p), err
		}
	}

	return len(p), nil
}

func cleanRunMessage(message map[string]any) map[string]any {
	result := make(map[string]any, len(message))

	for key, value := range message {
		switch key {
		case "scheduler_job", "scheduler_run_id", "caller":
			continue
		case "time":
			result[key] = cleanRunMessageTime(value)
		default:
			result[key] = value
		}
	}

	return result
}

func cleanRunMessageTime(value any) any {
	switch v := value.(type) {
	case float64:
		return time.UnixMilli(int64(v)).UTC().Format(time.RFC3339Nano)
	case int64:
		return time.UnixMilli(v).UTC().Format(time.RFC3339Nano)
	case int:
		return time.UnixMilli(int64(v)).UTC().Format(time.RFC3339Nano)
	default:
		return value
	}
}

func (w *RunWriter) Messages() []map[string]any {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.copyMessagesLocked()
}

func (w *RunWriter) Flush() error {
	if w.flush == nil {
		return nil
	}

	return w.flush(w.Messages())
}

func (w *RunWriter) copyMessagesLocked() []map[string]any {
	result := make([]map[string]any, len(w.messages))
	copy(result, w.messages)

	return result
}

func panicToError(value any) error {
	if err, ok := value.(error); ok {
		return err
	}

	return fmt.Errorf("panic: %v", value)
}
