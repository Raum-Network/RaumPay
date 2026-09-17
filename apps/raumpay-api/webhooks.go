package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"
)

type webhookEvent struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	CreatedAt string         `json:"created_at"`
	Data      map[string]any `json:"data"`
}

type webhookJob struct {
	event     webhookEvent
	body      []byte
	attempt   int
	nextAt    time.Time
	delivered bool
}

const (
	maxWebhookAttempts   = 5
	webhookBaseBackoff   = 2 * time.Second
	webhookClientTimeout = 5 * time.Second
)

// ponytail: in-memory queue with bounded exponential backoff + jitter, single
// dispatcher goroutine; state lost on restart. Swap for the worker + Postgres
// delivery-log when the persistence slice lands (Plan-2 §17).
func (a *api) startWebhookWorker(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(500 * time.Millisecond):
				a.dispatchDueWebhooks()
			}
		}
	}()
}

func (a *api) dispatchDueWebhooks() {
	a.mu.Lock()
	now := time.Now()
	due := make([]*webhookJob, 0, 4)
	for _, j := range a.webhookJobs {
		if !j.delivered && j.attempt < maxWebhookAttempts && j.nextAt.Before(now) {
			due = append(due, j)
		}
	}
	a.mu.Unlock()
	for _, j := range due {
		a.attemptDelivery(j)
	}
}

func (a *api) attemptDelivery(j *webhookJob) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, a.webhookSecret)
	mac.Write([]byte(ts + "." + string(j.body)))
	sig := hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequest(http.MethodPost, a.webhookURL, bytes.NewReader(j.body))
	if err != nil {
		slog.Error("webhook request build failed", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-ERupee-Event-ID", j.event.ID)
	req.Header.Set("X-ERupee-Timestamp", ts)
	req.Header.Set("X-ERupee-Signature", sig)

	resp, err := (&http.Client{Timeout: webhookClientTimeout}).Do(req)
	ok := err == nil && resp.StatusCode >= 200 && resp.StatusCode < 300
	if err != nil {
		slog.Warn("webhook delivery failed", "event_id", j.event.ID, "attempt", j.attempt+1, "error", err)
	} else {
		resp.Body.Close()
		if !ok {
			slog.Warn("webhook non-2xx", "event_id", j.event.ID, "attempt", j.attempt+1, "status", resp.StatusCode)
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	j.attempt++
	if ok {
		j.delivered = true
		return
	}
	if j.attempt >= maxWebhookAttempts {
		slog.Error("webhook permanently failed", "event_id", j.event.ID, "attempts", j.attempt)
		return
	}
	backoff := webhookBaseBackoff << (j.attempt - 1)
	backoff += time.Duration(rand.Int64N(int64(backoff / 2)))
	j.nextAt = time.Now().Add(backoff)
}

func (a *api) enqueueWebhook(p payment) {
	if a.webhookURL == "" {
		return
	}
	event := webhookEvent{
		ID:        "evt_" + p.ID + "_succeeded",
		Type:      "payment.succeeded",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Data: map[string]any{
			"payment_id":         p.ID,
			"merchant_reference": p.MerchantRef,
			"amount":             p.AmountPaise,
		},
	}
	body, err := json.Marshal(event)
	if err != nil {
		slog.Error("webhook marshal failed", "error", err)
		return
	}
	a.mu.Lock()
	a.webhookJobs = append(a.webhookJobs, &webhookJob{event: event, body: body, nextAt: time.Now()})
	a.mu.Unlock()
}
