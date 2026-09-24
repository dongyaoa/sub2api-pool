package poolupdate

import (
	"errors"
	"time"
)

var (
	ErrBusy          = errors.New("an image update is already running")
	ErrUnavailable   = errors.New("image updater requires operator recovery")
	ErrTargetChanged = errors.New("requested release no longer matches the latest verified release")
)

type UpdateRequest struct {
	Version string `json:"version"`
	Digest  string `json:"digest"`
}

type Job struct {
	ID         string     `json:"id"`
	State      string     `json:"state"`
	Message    string     `json:"message"`
	Version    string     `json:"version"`
	Revision   string     `json:"revision"`
	Digest     string     `json:"digest"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

type Status struct {
	Available        bool `json:"available"`
	RecoveryRequired bool `json:"recovery_required,omitempty"`
	Job              *Job `json:"job,omitempty"`
}

func terminal(state string) bool {
	return state == "succeeded" || state == "failed" || state == "rolled_back"
}
