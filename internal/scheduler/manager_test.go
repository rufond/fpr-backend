package scheduler

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func TestSchedulerDiagnosticsChanged(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		previousRun      *JobRun
		currentStatus    string
		previousRunKnown bool
		want             bool
	}{
		{
			name:             "successful run after successful run",
			previousRun:      &JobRun{Status: RunStatusCompleted},
			currentStatus:    RunStatusCompleted,
			previousRunKnown: true,
			want:             false,
		},
		{
			name:             "noop run after successful run",
			previousRun:      &JobRun{Status: RunStatusCompleted},
			currentStatus:    RunStatusNoop,
			previousRunKnown: true,
			want:             false,
		},
		{
			name:             "new failed run",
			previousRun:      &JobRun{Status: RunStatusCompleted},
			currentStatus:    RunStatusFailed,
			previousRunKnown: true,
			want:             true,
		},
		{
			name:             "repeated failed run",
			previousRun:      &JobRun{Status: RunStatusFailed},
			currentStatus:    RunStatusFailed,
			previousRunKnown: true,
			want:             true,
		},
		{
			name:             "successful run clears previous failure",
			previousRun:      &JobRun{Status: RunStatusFailed},
			currentStatus:    RunStatusCompleted,
			previousRunKnown: true,
			want:             true,
		},
		{
			name:             "noop run clears previous failure",
			previousRun:      &JobRun{Status: RunStatusFailed},
			currentStatus:    RunStatusNoop,
			previousRunKnown: true,
			want:             true,
		},
		{
			name:             "first successful run",
			currentStatus:    RunStatusCompleted,
			previousRunKnown: true,
			want:             false,
		},
		{
			name:             "unknown previous state is conservative",
			currentStatus:    RunStatusCompleted,
			previousRunKnown: false,
			want:             true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := schedulerDiagnosticsChanged(tt.previousRun, tt.currentStatus, tt.previousRunKnown)
			if got != tt.want {
				t.Fatalf("schedulerDiagnosticsChanged() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLogRunFinishedLevel(t *testing.T) {
	tests := []struct {
		name      string
		runSource string
		status    string
		wantLevel string
	}{
		{name: "scheduled completed", runSource: RunSourceSchedule, status: RunStatusCompleted, wantLevel: "debug"},
		{name: "scheduled noop", runSource: RunSourceSchedule, status: RunStatusNoop, wantLevel: "debug"},
		{name: "manual completed", runSource: RunSourceManual, status: RunStatusCompleted, wantLevel: "info"},
		{name: "scheduled failed", runSource: RunSourceSchedule, status: RunStatusFailed, wantLevel: "error"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			previousLogger := log.Logger
			defer func() { log.Logger = previousLogger }()

			var output bytes.Buffer
			log.Logger = zerolog.New(&output).Level(zerolog.DebugLevel)

			manager := &Manager{}
			manager.logRunFinished("test", 1, test.runSource, test.status, nil, "")

			var event struct {
				Level string `json:"level"`
			}
			if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &event); err != nil {
				t.Fatalf("decode log event: %v", err)
			}
			if event.Level != test.wantLevel {
				t.Fatalf("level = %q, want %q", event.Level, test.wantLevel)
			}
		})
	}
}
