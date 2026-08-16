package scheduler

import "testing"

func TestRunWriterMessagesReturnsEmptySlice(t *testing.T) {
	t.Parallel()

	writer := NewRunWriter(0, nil)
	messages := writer.Messages()

	if messages == nil {
		t.Fatal("Messages() returned nil, want empty slice")
	}
	if len(messages) != 0 {
		t.Fatalf("Messages() len = %d, want 0", len(messages))
	}
}
