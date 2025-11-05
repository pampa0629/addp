package logging

import (
    "context"
    "fmt"
    "log/slog"
    "os"
    "time"

    "github.com/addp/transfer/internal/repository"
)

// DBLogHandler is a slog.Handler that forwards logs to a base handler and
// additionally appends records containing an "execution_id" attribute to
// the corresponding execution's Logs field in database.
type DBLogHandler struct {
    base     slog.Handler
    execRepo *repository.ExecutionRepository
}

// NewDBLogHandler creates a DBLogHandler wrapping the provided base handler.
func NewDBLogHandler(base slog.Handler, execRepo *repository.ExecutionRepository) *DBLogHandler {
    return &DBLogHandler{base: base, execRepo: execRepo}
}

func (h *DBLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
    return h.base.Enabled(ctx, level)
}

func (h *DBLogHandler) Handle(ctx context.Context, r slog.Record) error {
    // Always forward to base handler (console/file)
    if err := h.base.Handle(ctx, r); err != nil {
        // do not early-return; still try DB append
        _ = err
    }

    // Extract execution_id; if present, append a formatted line to DB
    var execID uint64
    hasExec := false
    attrs := make([]slog.Attr, 0, r.NumAttrs())
    r.Attrs(func(a slog.Attr) bool {
        attrs = append(attrs, a)
        if a.Key == "execution_id" {
            // Try to read as uint64; other numeric types will be coerced by String
            if v := a.Value; v.Kind() == slog.KindUint64 {
                execID = v.Uint64()
                hasExec = true
            } else if v.Kind() == slog.KindInt64 {
                // convert signed -> unsigned best-effort if non-negative
                iv := v.Int64()
                if iv >= 0 {
                    execID = uint64(iv)
                    hasExec = true
                }
            } else {
                // fallback: parse from string if needed
                // Best-effort: ignore if cannot parse
                if s := v.String(); s != "" {
                    var tmp uint64
                    _, _ = fmt.Sscanf(s, "%d", &tmp)
                    if tmp > 0 {
                        execID = tmp
                        hasExec = true
                    }
                }
            }
        }
        return true
    })

    if hasExec && h.execRepo != nil {
        // Build a compact single-line message: [time level] message key=val ...
        ts := r.Time
        if ts.IsZero() {
            ts = time.Now()
        }
        line := fmt.Sprintf("[%s %s] %s", ts.Format(time.RFC3339), r.Level.String(), r.Message)
        for _, a := range attrs {
            if a.Key == "execution_id" {
                continue
            }
            line += fmt.Sprintf(" %s=%s", a.Key, a.Value.String())
        }
        // Append to DB (best-effort; ignore error to avoid blocking pipeline)
        _ = h.execRepo.AppendLog(uint(execID), line)
    }
    return nil
}

func (h *DBLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
    return &DBLogHandler{base: h.base.WithAttrs(attrs), execRepo: h.execRepo}
}

func (h *DBLogHandler) WithGroup(name string) slog.Handler {
    return &DBLogHandler{base: h.base.WithGroup(name), execRepo: h.execRepo}
}

// Helper to instantiate a default text-based base handler to stdout when needed.
func NewStdoutTextDBLogger(execRepo *repository.ExecutionRepository) *slog.Logger {
    base := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
    return slog.New(NewDBLogHandler(base, execRepo))
}
