package customsip

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Bootstrap loads the full custom-SIP config from voice-agent.
func Bootstrap(ctx context.Context, url string, store *Store, client *http.Client) (int, error) {
	if url == "" {
		return 0, fmt.Errorf("bootstrap URL is empty")
	}
	if store == nil {
		return 0, fmt.Errorf("custom-sip store is nil")
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, err
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("bootstrap HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	cfg, err := DecodeConfigJSON(bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	cfg.Apply(store)
	return store.Size(), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
