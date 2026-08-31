package customsip

import (
	"context"
	"net/http"
	"time"

	"github.com/livekit/protocol/logger"
)

// BootstrapLoop tries Bootstrap once immediately, then retries with backoff until
// success or ctx is cancelled. onResult is called after each attempt.
func BootstrapLoop(
	ctx context.Context,
	log logger.Logger,
	url string,
	store *Store,
	onResult func(size int, err error),
) {
	if url == "" || store == nil {
		return
	}
	client := &http.Client{Timeout: 10 * time.Second}
	attempt := 0
	backoff := time.Second
	const maxBackoff = 60 * time.Second

	for {
		attempt++
		size, err := Bootstrap(ctx, url, store, client)
		if onResult != nil {
			onResult(size, err)
		}
		if err == nil {
			if log != nil {
				log.Infow("direct-sip routes bootstrapped", "size", size, "attempt", attempt)
			}
			return
		}
		if log != nil {
			log.Errorw("direct-sip routes bootstrap failed; retrying", err, "attempt", attempt)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}
