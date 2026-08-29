package analytics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

const (
	// queueDepth is how many events may be waiting to be sent. Past this they
	// are dropped, which is the entire design: the alternative to dropping an
	// event is blocking the request that produced it, and no measurement is
	// worth making a lesson slower to check.
	queueDepth = 1024

	// batchSize and flushInterval trade latency for request count. Neither
	// number matters much; both being bounded does.
	batchSize     = 50
	flushInterval = 5 * time.Second

	sendTimeout = 10 * time.Second
)

type postHog struct {
	apiKey string
	url    string
	http   *http.Client
	log    *slog.Logger

	queue chan Event
	done  chan struct{}
}

func newPostHog(apiKey, host string, log *slog.Logger) *postHog {
	p := &postHog{
		apiKey: apiKey,
		url:    host + "/batch/",
		http:   &http.Client{Timeout: sendTimeout},
		log:    log,
		queue:  make(chan Event, queueDepth),
		done:   make(chan struct{}),
	}
	go p.loop()
	return p
}

// Capture queues an event, or drops it if the queue is full.
//
// The default branch is what makes this non-blocking, and it is load-bearing
// rather than defensive: when the analytics endpoint is slow, every request
// handler in the process would otherwise be waiting on it.
func (p *postHog) Capture(e Event) {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	select {
	case p.queue <- e:
	default:
		// Logged at debug: a dropped event is expected under load and is not
		// worth waking anyone up for.
		p.log.Debug("analytics queue full, event dropped", slog.String("event", e.Name))
	}
}

// Close flushes what is queued and stops the worker. It is bounded by ctx, so a
// hung analytics endpoint cannot hold up the process's shutdown.
func (p *postHog) Close(ctx context.Context) error {
	close(p.queue)
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *postHog) loop() {
	defer close(p.done)

	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	batch := make([]Event, 0, batchSize)

	for {
		select {
		case e, ok := <-p.queue:
			if !ok {
				p.send(batch)
				return
			}
			batch = append(batch, e)
			if len(batch) >= batchSize {
				p.send(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				p.send(batch)
				batch = batch[:0]
			}
		}
	}
}

func (p *postHog) send(batch []Event) {
	if len(batch) == 0 {
		return
	}

	type wireEvent struct {
		Event      string         `json:"event"`
		DistinctID string         `json:"distinct_id"`
		Properties map[string]any `json:"properties,omitempty"`
		Timestamp  string         `json:"timestamp"`
	}

	payload := struct {
		APIKey string      `json:"api_key"`
		Batch  []wireEvent `json:"batch"`
	}{APIKey: p.apiKey, Batch: make([]wireEvent, 0, len(batch))}

	for _, e := range batch {
		payload.Batch = append(payload.Batch, wireEvent{
			Event:      e.Name,
			DistinctID: e.DistinctID,
			Properties: e.Props,
			Timestamp:  e.Timestamp.UTC().Format(time.RFC3339),
		})
	}

	body, err := json.Marshal(payload)
	if err != nil {
		p.log.Error("analytics: encode failed", slog.Any("error", err))
		return
	}

	// Background context, not a request's: this runs on the worker, long after
	// the request that produced the events has been answered.
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(body))
	if err != nil {
		p.log.Error("analytics: request failed", slog.Any("error", err))
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.http.Do(req)
	if err != nil {
		// Warn, not error. Losing analytics is not an incident, and paging on
		// it would train whoever reads these to ignore the channel.
		p.log.Warn("analytics: send failed", slog.Any("error", err), slog.Int("events", len(batch)))
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= http.StatusBadRequest {
		p.log.Warn("analytics: rejected",
			slog.Int("status", resp.StatusCode),
			slog.Int("events", len(batch)),
			slog.String("detail", fmt.Sprintf("POST %s", p.url)),
		)
	}
}
