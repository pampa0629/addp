// Package logpipeline defines the bounded Infra-to-Monitor observation contract.
// It carries safe counters only, never runtime log content.
package logpipeline

import (
	"errors"
	"regexp"
	"time"
)

const Schema = "addp.log-observation/v1"
const MaxReceivers = 512

var Identity = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,99}$`)

type Receiver struct {
	Module        string    `json:"module"`
	InstanceID    string    `json:"instance_id"`
	ObservedAt    time.Time `json:"observed_at"`
	Received      uint64    `json:"received"`
	Written       uint64    `json:"written"`
	Dropped       uint64    `json:"dropped"`
	WriteFailures uint64    `json:"write_failures"`
	ParseFailures uint64    `json:"parse_failures"`
	EarlyCleaned  uint64    `json:"source_files_early_cleaned"`
	Truncated     uint64    `json:"truncated"`
}
type Collector struct {
	Valid     bool    `json:"valid"`
	StartedAt float64 `json:"started_at"`
	Retries   uint64  `json:"retries"`
	Dropped   uint64  `json:"dropped"`
	Sent      uint64  `json:"sent"`
}
type Observation struct {
	Schema            string     `json:"schema"`
	Node              string     `json:"node"`
	BootID            string     `json:"boot_id"`
	Sequence          uint64     `json:"sequence"`
	SampledAt         time.Time  `json:"sampled_at"`
	APIReady          bool       `json:"api_ready"`
	ProbeDelivered    bool       `json:"probe_delivered"`
	ProbeDelayMS      int64      `json:"probe_delay_ms"`
	Collector         Collector  `json:"collector"`
	SourcesValid      bool       `json:"sources_valid"`
	SourceBytes       int64      `json:"source_bytes"`
	SourceLimitBytes  int64      `json:"source_limit_bytes"`
	HousekeepingValid bool       `json:"housekeeping_valid"`
	HousekeepingAt    time.Time  `json:"housekeeping_at"`
	EarlyCleaned      uint64     `json:"source_files_early_cleaned"`
	QuotaExhausted    bool       `json:"quota_exhausted"`
	Receivers         []Receiver `json:"receivers"`
}

func (o Observation) Validate(now time.Time) error {
	if o.Schema != Schema || !Identity.MatchString(o.Node) || !Identity.MatchString(o.BootID) || o.Sequence == 0 || o.SampledAt.IsZero() || o.SampledAt.Before(now.Add(-time.Minute)) || o.SampledAt.After(now.Add(30*time.Second)) || len(o.Receivers) > MaxReceivers || o.SourceBytes < 0 || o.SourceLimitBytes <= 0 || o.ProbeDelayMS < 0 || o.ProbeDelayMS > 60000 || (o.Collector.Valid && o.Collector.StartedAt <= 0) {
		return errors.New("invalid log observation")
	}
	seen := map[string]bool{}
	for _, r := range o.Receivers {
		if !Identity.MatchString(r.Module) || !Identity.MatchString(r.InstanceID) || seen[r.InstanceID] || r.ObservedAt.IsZero() || r.ObservedAt.After(now.Add(30*time.Second)) {
			return errors.New("invalid receiver observation")
		}
		seen[r.InstanceID] = true
	}
	return nil
}
