package scheduler

import "testing"

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
