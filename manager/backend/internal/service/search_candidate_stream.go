package service

import (
	"context"
	"unsafe"
)

type searchRawStream interface {
	read(context.Context, *searchStep) ([]searchSourceHit, error)
}
type searchAuthorizedStream struct {
	session      *searchSession
	raw          searchRawStream
	pending      []searchSourceHit
	pendingBytes int64
	buffer       []searchSourceHit
	bufferBytes  int64
	position     int
	checked      uint64
	exhausted    bool
}

func (s *searchAuthorizedStream) peek(ctx context.Context, step *searchStep) (searchSourceHit, bool, error) {
	for {
		if s.position < len(s.buffer) {
			if s.checked != step.number {
				allowed, err := step.authorize(ctx, s.buffer[s.position:])
				if err != nil {
					return searchSourceHit{}, false, err
				}
				for _, ok := range allowed {
					if !ok {
						return searchSourceHit{}, false, ErrSearchContextChanged
					}
				}
				s.checked = step.number
			}
			return s.buffer[s.position], true, nil
		}
		if s.buffer != nil {
			_ = s.session.reserve(-s.bufferBytes)
			s.buffer = nil
			s.bufferBytes = 0
			s.position = 0
		}
		if s.exhausted {
			return searchSourceHit{}, false, nil
		}
		if s.pending == nil {
			raw, err := s.raw.read(ctx, step)
			if len(raw) > 0 {
				size := int64(cap(raw)) * int64(unsafe.Sizeof(searchSourceHit{}))
				if reserveErr := s.session.reserve(size); reserveErr != nil {
					return searchSourceHit{}, false, reserveErr
				}
				s.pending = raw
				s.pendingBytes = size
			}
			if err != nil {
				if err == errSearchStepBudget && len(raw) > 0 { /* consume the retained partial batch first */
				} else {
					return searchSourceHit{}, false, err
				}
			}
			if len(raw) == 0 {
				s.exhausted = true
				return searchSourceHit{}, false, nil
			}
		}
		allowed, err := step.authorize(ctx, s.pending)
		if err != nil {
			return searchSourceHit{}, false, err
		}
		// Reuse the owned input allocation; it contains identities and ranks only.
		raw := s.pending
		s.pending = nil
		s.pendingBytes = 0
		filtered := raw[:0]
		for i, hit := range raw {
			if allowed[i] {
				filtered = append(filtered, hit)
			}
		}
		clear(raw[len(filtered):])
		s.buffer = filtered
		s.bufferBytes = int64(cap(raw)) * int64(unsafe.Sizeof(searchSourceHit{}))
		s.position = 0
		s.checked = step.number
	}
}
func (s *searchAuthorizedStream) consume() { s.position++ }
