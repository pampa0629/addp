package auditarchive

import (
	"context"
	"time"
)

type writer struct {
	store                   Store
	source                  Source
	days                    int
	current                 *segment
	rows                    []Row
	transactions            []Transaction
	buffered                int
	lastFlush               time.Time
	confirmedCount          int
	lastCommitAt            time.Time
	partLimit, segmentBytes int // Owner-test bounds, never deployment-controlled.
	now                     func() time.Time
}

func newWriter(s Store, src Source, days int, g *segment) *writer {
	return &writer{store: s, source: src, days: days, current: g, partLimit: maxParts, segmentBytes: segmentTarget, now: func() time.Time { return time.Now().UTC() }}
}
func (w *writer) add(ctx context.Context, rows []Row, end uint64, at time.Time) error {
	if w.days < minRetentionDays || w.days > 36500 {
		return ErrUnknown
	}
	if len(rows) == 0 {
		return ErrUnknown
	}
	size := 0
	rawBytes := 0
	for _, r := range rows {
		rawBytes += len(auditColumns) * 32
		for _, v := range r {
			if v != nil {
				rawBytes += len(*v)
			}
		}
		if !validRow(r) {
			return ErrUnknown
		}
		b, e := encode(r)
		if e != nil {
			return e
		}
		size += len(b)
	}
	if rawBytes > MaxObjectBytes/2 || size > MaxObjectBytes-4096 {
		return ErrBudget
	}
	if w.buffered > 0 && w.buffered+size > partTarget {
		if e := w.flush(ctx); e != nil {
			return e
		}
	}
	if !w.current.Baseline {
		last := w.current.EndLSN
		if len(w.transactions) > 0 {
			last = w.transactions[len(w.transactions)-1].EndLSN
		}
		if end <= last || at.IsZero() {
			return ErrConflict
		}
		w.transactions = append(w.transactions, Transaction{end, at, len(rows)})
	} else if end != w.current.EndLSN {
		return ErrConflict
	}
	if w.lastFlush.IsZero() {
		w.lastFlush = w.now()
	}
	w.rows = append(w.rows, rows...)
	w.buffered += size
	if w.buffered >= partTarget {
		return w.flush(ctx)
	}
	return nil
}
func (w *writer) flush(ctx context.Context) error {
	if len(w.rows) == 0 {
		g := w.current
		if len(g.Parts) > 0 && (len(g.Parts) >= w.partLimit || g.Bytes >= w.segmentBytes || g.Day != w.now().Format("2006-01-02")) {
			_, e := w.seal(ctx)
			return e
		}
		return nil
	}
	g := w.current
	day := w.now().Format("2006-01-02")
	if len(g.Parts) > 0 && (len(g.Parts) >= w.partLimit || g.Bytes >= w.segmentBytes || g.Day != day) {
		if _, e := w.seal(ctx); e != nil {
			return e
		}
		g = w.current
	}
	end := g.EndLSN
	if len(w.transactions) > 0 {
		end = w.transactions[len(w.transactions)-1].EndLSN
	}
	prev := ""
	if len(g.Parts) > 0 {
		prev = g.Parts[len(g.Parts)-1].Ref.SHA
	}
	p := Part{format, w.source, g.Sequence, len(g.Parts), prev, g.Previous, g.Baseline, g.EndLSN, end, w.now(), w.transactions, w.rows}
	b, e := encode(p)
	if e != nil {
		return e
	}
	until := w.now().Add(time.Duration(w.days)*24*time.Hour + retentionMargin)
	r, e := put(ctx, w.store, partKey(w.source.ID, g.Sequence, len(g.Parts)), p, until, false)
	if e != nil {
		return e
	}
	w.confirmedCount += len(w.rows)
	if len(w.transactions) > 0 {
		w.lastCommitAt = w.transactions[len(w.transactions)-1].At
	}
	g.Parts = append(g.Parts, Entry{r, len(b), len(w.rows), end})
	g.EndLSN = end
	g.Bytes += len(b)
	g.Day = day
	w.rows = nil
	w.transactions = nil
	w.buffered = 0
	w.lastFlush = w.now()
	return nil
}
func (w *writer) seal(ctx context.Context) (Ref, error) {
	g := w.current
	if len(g.Parts) == 0 && !g.Baseline {
		return Ref{}, ErrUnknown
	}
	// Select one common deadline, extending only. Publication occurs last.
	closed := w.now()
	until := retentionUntil(closed.Add(time.Duration(w.days)*24*time.Hour + retentionMargin))
	for _, entry := range g.Parts {
		_, pr, e := readRef(ctx, w.store, entry.Ref, false)
		if e != nil {
			return Ref{}, e
		}
		if pr.Until.After(until) {
			until = retentionUntil(pr.Until)
		}
	}
	for _, entry := range g.Parts {
		v := Version{Key: entry.Ref.Key, ID: entry.Ref.Version}
		writeErr := w.store.Extend(ctx, v, until)
		_, pr, e := readRef(ctx, w.store, entry.Ref, false)
		if e != nil || pr.Until.Before(until) {
			if writeErr != nil {
				return Ref{}, ErrUnknown
			}
			return Ref{}, ErrUnknown
		}
	}
	if w.now().After(closed.Add(retentionMargin / 2)) {
		return Ref{}, ErrUnknown
	}
	cp := Checkpoint{format, w.source, g.Sequence, g.Previous, g.Baseline, g.BeforeLSN, g.EndLSN, closed, until, g.Parts}
	r, e := put(ctx, w.store, checkpointKey(w.source.ID, g.Sequence), cp, until, true)
	if e != nil {
		return Ref{}, e
	}
	w.current = &segment{Sequence: g.Sequence + 1, Previous: r, Baseline: g.Baseline, BeforeLSN: g.EndLSN, EndLSN: g.EndLSN}
	return r, nil
}
