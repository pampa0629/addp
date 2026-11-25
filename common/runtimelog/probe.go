package runtimelog

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Technical sources retain recent diagnostic events, not business history.
const probeSourceAge = time.Hour

// ProbeReceiver owns one Capture lifetime. A detection event is not a source
// instance: repeated detections must preserve both source identity and sequence.
type ProbeReceiver struct {
	o        Options
	input    *probeInput
	busy     chan struct{}
	closed   sync.Once
	done     chan error
	ctx      context.Context
	cancel   context.CancelFunc
	closeErr error
}

func NewProbeReceiver(o Options) *ProbeReceiver {
	o.Module = "runtime-probe"
	o.InstanceID = uuid.NewString()
	o.Role = "backend"
	ctx, cancel := context.WithCancel(context.Background())
	p := &ProbeReceiver{o: o, input: &probeInput{events: make(chan []byte, 1), closed: ctx.Done()}, busy: make(chan struct{}, 1), done: make(chan error, 1), ctx: ctx, cancel: cancel}
	go func() { p.done <- Capture(o, p.input, strings.NewReader("")) }()
	return p
}

// Close stops input and waits for Capture to flush and release its segment lock.
func (p *ProbeReceiver) Close() error {
	p.closed.Do(func() {
		p.cancel()
		p.closeErr = <-p.done
	})
	return p.closeErr
}

// probeInput admits at most one bounded diagnostic message without blocking a
// caller on filesystem I/O. Only Capture reads pending; Close drains admitted input.
type probeInput struct {
	events  chan []byte
	closed  <-chan struct{}
	pending []byte
}

func (r *probeInput) Read(dst []byte) (int, error) {
	if len(r.pending) == 0 {
		select {
		case r.pending = <-r.events:
		case <-r.closed:
			select {
			case r.pending = <-r.events:
			default:
				return 0, io.EOF
			}
		}
	}
	n := copy(dst, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

// Probe writes through the normal receiver and waits for this exact new event.
func (p *ProbeReceiver) Probe(ctx context.Context, confirm func(context.Context, string, string) (bool, error)) (time.Duration, error) {
	started := time.Now()
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(p.ctx, cancel)
	defer func() { stop(); cancel() }()
	if confirm == nil {
		return 0, fmt.Errorf("probe requires System confirmation")
	}
	select {
	case p.busy <- struct{}{}:
		defer func() { <-p.busy }()
	case <-ctx.Done():
		return time.Since(started), ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return time.Since(started), err
	}
	select {
	case <-p.input.closed:
		return time.Since(started), io.ErrClosedPipe
	default:
	}
	marker := uuid.NewString()
	message := "runtime-log-delivery-probe " + marker
	select {
	case p.input.events <- []byte(message + "\n"):
	case <-p.input.closed:
		return time.Since(started), io.ErrClosedPipe
	case <-ctx.Done():
		return time.Since(started), ctx.Err()
	}
	for {
		delivered, err := confirm(ctx, p.o.InstanceID, marker)
		if err != nil {
			return time.Since(started), err
		}
		if delivered {
			return time.Since(started), nil
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return time.Since(started), ctx.Err()
		case <-timer.C:
		}
	}
}
