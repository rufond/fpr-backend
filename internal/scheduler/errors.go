package scheduler

import "errors"

var (
	ErrJobNotFound       = errors.New("scheduler job not found")
	ErrJobAlreadyRunning = errors.New("scheduler job already running")
)
