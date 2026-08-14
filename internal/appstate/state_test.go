package appstate

import (
	"errors"
	"testing"
)

func TestManagerInitializeAndLoad(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	state := &State{Fund: &FundState{}}

	if err := manager.Initialize(state); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if manager.Load() != state {
		t.Fatal("Load() did not return initialized state")
	}
}

func TestManagerUpdatePublishesReturnedState(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	initial := &State{Fund: &FundState{}}
	if err := manager.Initialize(initial); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	next := &State{Fund: &FundState{}}
	if err := manager.Update(func(current *State) (*State, error) {
		if current != initial {
			t.Fatal("Update() received unexpected current state")
		}
		return next, nil
	}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	if manager.Load() != next {
		t.Fatal("Load() did not return updated state")
	}
}

func TestManagerUpdateDoesNotPublishOnError(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	initial := &State{Fund: &FundState{}}
	if err := manager.Initialize(initial); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	wantErr := errors.New("persist failed")
	err := manager.Update(func(_ *State) (*State, error) {
		return &State{Fund: &FundState{}}, wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Update() error = %v, want %v", err, wantErr)
	}
	if manager.Load() != initial {
		t.Fatal("state changed after failed update")
	}
}
