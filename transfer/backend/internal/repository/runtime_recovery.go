package repository

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	commonExecution "github.com/addp/common/execution"
)

const (
	continuousRecoveryReasonWorkerShutdown  = "worker_shutdown"
	continuousRecoveryReasonLeaseExpired    = "lease_expired"
	continuousRecoveryReasonExecutionFailed = "execution_failed"

	recoveryCircuitClosed   = "closed"
	recoveryCircuitOpen     = "open"
	recoveryCircuitHalfOpen = "half_open"
)

type ContinuousRecoveryPolicy struct {
	InitialBackoff  time.Duration
	MaxBackoff      time.Duration
	MaxFailures     int
	CircuitOpenTime time.Duration
	StabilityWindow time.Duration
}

func (p ContinuousRecoveryPolicy) Validate() error {
	if p.InitialBackoff <= 0 || p.MaxBackoff <= 0 || p.CircuitOpenTime <= 0 || p.StabilityWindow <= 0 {
		return fmt.Errorf("continuous recovery durations must be greater than zero")
	}
	if p.InitialBackoff > p.MaxBackoff {
		return fmt.Errorf("continuous recovery initial backoff must not exceed max backoff")
	}
	if p.MaxFailures <= 0 {
		return fmt.Errorf("continuous recovery max failures must be greater than zero")
	}
	return nil
}

type continuousRecoveryPlan struct {
	Attempt      int
	NotBefore    time.Time
	Backoff      time.Duration
	CircuitState string
}

func buildContinuousRecoveryPlan(previous commonExecution.TaskExecution, reason string, sessionStartedAt, now time.Time, policy ContinuousRecoveryPolicy) continuousRecoveryPlan {
	previousAttempt := recoveryMetadataInt(previous.Metadata["recovery_consecutive_failures"])
	previousCircuit, _ := previous.Metadata["recovery_circuit_state"].(string)
	if previousCircuit == "" {
		previousCircuit = recoveryCircuitClosed
	}
	if reason == continuousRecoveryReasonWorkerShutdown {
		return continuousRecoveryPlan{
			Attempt: previousAttempt, NotBefore: now, CircuitState: previousCircuit,
		}
	}

	attempt := previousAttempt + 1
	if sessionStartedAt.IsZero() || now.Sub(sessionStartedAt) >= policy.StabilityWindow {
		attempt = 1
		previousCircuit = recoveryCircuitClosed
	}
	if previousCircuit == recoveryCircuitHalfOpen || attempt >= policy.MaxFailures {
		return continuousRecoveryPlan{
			Attempt: policy.MaxFailures, NotBefore: now.Add(policy.CircuitOpenTime),
			Backoff: policy.CircuitOpenTime, CircuitState: recoveryCircuitOpen,
		}
	}

	backoff := policy.InitialBackoff
	for i := 1; i < attempt && backoff < policy.MaxBackoff; i++ {
		if backoff > policy.MaxBackoff/2 {
			backoff = policy.MaxBackoff
			break
		}
		backoff *= 2
	}
	if backoff > policy.MaxBackoff {
		backoff = policy.MaxBackoff
	}
	return continuousRecoveryPlan{
		Attempt: attempt, NotBefore: now.Add(backoff), Backoff: backoff, CircuitState: recoveryCircuitClosed,
	}
}

func recoveryMetadataInt(value interface{}) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case uint64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return int(parsed)
	case string:
		parsed, _ := strconv.Atoi(typed)
		return parsed
	default:
		return 0
	}
}
