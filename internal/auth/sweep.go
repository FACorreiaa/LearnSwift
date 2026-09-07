package auth

import (
	"context"
	"log/slog"
	"time"
)

// sweepInterval is how often expired credentials are cleared out. The rows are
// already ignored by the lookup queries the moment they expire, so this is
// housekeeping rather than security — hourly is frequent enough that the tables
// cannot grow without bound, and rare enough to be invisible.
const sweepInterval = time.Hour

// expiryStore is the one thing a sweepable table has in common. Both stores
// satisfy it, which is what lets one loop serve both rather than two loops
// differing only in a noun.
type expiryStore interface {
	DeleteExpired(ctx context.Context) (int64, error)
}

// SweepExpiredSessions deletes expired rows until ctx is cancelled.
//
// It runs in the web process rather than a worker because it is one statement
// an hour against an indexed column. If several replicas run it at once they
// simply delete the same already-dead rows, so it needs no leader election —
// which is the entire reason it can live here instead of becoming another
// deployable.
func SweepExpiredSessions(ctx context.Context, store *SessionStore, log *slog.Logger) {
	sweep(ctx, store, "sessions", log)
}

// SweepExpiredAPITokens does the same for access tokens. Separate from the
// session sweep rather than folded into it, so that a deployment without the
// MCP endpoint enabled simply never starts it.
func SweepExpiredAPITokens(ctx context.Context, store *TokenStore, log *slog.Logger) {
	sweep(ctx, store, "api tokens", log)
}

func sweep(ctx context.Context, store expiryStore, what string, log *slog.Logger) {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()

	// Once at startup, so a process that is restarted more often than the
	// interval still clears up rather than never sweeping at all.
	sweepOnce(ctx, store, what, log)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepOnce(ctx, store, what, log)
		}
	}
}

func sweepOnce(ctx context.Context, store expiryStore, what string, log *slog.Logger) {
	// Bounded independently of the caller's context: a sweep that hangs must
	// not hold the loop, and there is always another one along in an hour.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	removed, err := store.DeleteExpired(ctx)
	if err != nil {
		// Never fatal. Failing to tidy up is not a reason to take down a
		// process that is serving requests perfectly well.
		log.Warn("could not sweep expired credentials", slog.String("kind", what), slog.Any("error", err))
		return
	}
	if removed > 0 {
		log.Info("swept expired credentials", slog.String("kind", what), slog.Int64("removed", removed))
	}
}
