package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testMerchantKey  = "merchant-test-key"
	testSimulatorKey = "simulator-test-key"
)

func newTestAPI(t *testing.T) *api {
	t.Helper()
	a, err := newAPI(testMerchantKey, testSimulatorKey, "", "", &store{})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func doJSON(t *testing.T, h http.Handler, method, path, key, idem, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func confirmFlow(t *testing.T, h http.Handler, id string) string {
	t.Helper()
	for i := 0; i < 3; i++ {
		rec := doJSON(t, h, http.MethodPost, "/v1/payments/"+id+"/confirm", testSimulatorKey, "", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("confirm step %d: got %d", i+1, rec.Code)
		}
	}
	return ""
}

func TestCreatePaymentRequiresAuth(t *testing.T) {
	h := newTestAPI(t).handler()
	rec := doJSON(t, h, http.MethodPost, "/v1/payments", "", "k1", `{"amount":2500,"currency":"INR","merchant_reference":"ORDER-123"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func TestCreatePaymentValidation(t *testing.T) {
	h := newTestAPI(t).handler()
	rec := doJSON(t, h, http.MethodPost, "/v1/payments", testMerchantKey, "k2", `{"amount":0,"currency":"INR","merchant_reference":"ORDER-123"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
	rec = doJSON(t, h, http.MethodPost, "/v1/payments", testMerchantKey, "", `{"amount":2500,"currency":"INR","merchant_reference":"ORDER-123"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing idempotency key: want 400, got %d", rec.Code)
	}
}

func TestIdempotentReplaySameBody(t *testing.T) {
	h := newTestAPI(t).handler()
	body := `{"amount":2500,"currency":"INR","merchant_reference":"ORDER-123"}`
	first := doJSON(t, h, http.MethodPost, "/v1/payments", testMerchantKey, "k3", body)
	second := doJSON(t, h, http.MethodPost, "/v1/payments", testMerchantKey, "k3", body)
	if first.Code != http.StatusCreated || second.Code != http.StatusOK {
		t.Fatalf("codes %d %d", first.Code, second.Code)
	}
	var p1, p2 struct{ ID string }
	json.Unmarshal(first.Body.Bytes(), &p1)
	json.Unmarshal(second.Body.Bytes(), &p2)
	if p1.ID == "" || p1.ID != p2.ID {
		t.Fatalf("idempotent replay returned different payment: %q vs %q", p1.ID, p2.ID)
	}
}

func TestIdempotencyKeyBodyMismatch(t *testing.T) {
	h := newTestAPI(t).handler()
	doJSON(t, h, http.MethodPost, "/v1/payments", testMerchantKey, "k4", `{"amount":2500,"currency":"INR","merchant_reference":"A"}`)
	rec := doJSON(t, h, http.MethodPost, "/v1/payments", testMerchantKey, "k4", `{"amount":9999,"currency":"INR","merchant_reference":"B"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d", rec.Code)
	}
}

func TestFullFlowToSucceeded(t *testing.T) {
	h := newTestAPI(t).handler()
	rec := doJSON(t, h, http.MethodPost, "/v1/payments", testMerchantKey, "k5", `{"amount":1250,"currency":"INR","merchant_reference":"POS-91827"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: got %d", rec.Code)
	}
	var created struct {
		ID     string
		Status string
	}
	json.Unmarshal(rec.Body.Bytes(), &created)
	if created.ID == "" || created.Status != statusCreated {
		t.Fatalf("bad create response: %+v", created)
	}
	confirmFlow(t, h, created.ID)
	final := doJSON(t, h, http.MethodGet, "/v1/payments/"+created.ID, testMerchantKey, "", "")
	if final.Code != http.StatusOK {
		t.Fatalf("get: got %d", final.Code)
	}
	var done struct {
		Status string
	}
	json.Unmarshal(final.Body.Bytes(), &done)
	if done.Status != statusSucceeded {
		t.Fatalf("want SUCCEEDED, got %q", done.Status)
	}
}

func TestSimulatorKeyCannotCreateOrRead(t *testing.T) {
	h := newTestAPI(t).handler()
	rec := doJSON(t, h, http.MethodPost, "/v1/payments", testSimulatorKey, "k6", `{"amount":100,"currency":"INR","merchant_reference":"X"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("simulator key must not create: got %d", rec.Code)
	}
	rec = doJSON(t, h, http.MethodGet, "/v1/payments/pay_er_abc", testSimulatorKey, "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("simulator key must not read: got %d", rec.Code)
	}
}

func TestGetMissingPayment(t *testing.T) {
	h := newTestAPI(t).handler()
	rec := doJSON(t, h, http.MethodGet, "/v1/payments/pay_er_nope", testMerchantKey, "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", rec.Code)
	}
}

func TestConcurrentIdempotentCreates(t *testing.T) {
	a := newTestAPI(t)
	h := a.handler()
	body := `{"amount":500,"currency":"INR","merchant_reference":"RACE-1"}`
	const n = 20
	var wg sync.WaitGroup
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec := doJSON(t, h, http.MethodPost, "/v1/payments", testMerchantKey, "race-key", body)
			if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
				t.Errorf("got %d", rec.Code)
				return
			}
			var p struct{ ID string }
			json.Unmarshal(rec.Body.Bytes(), &p)
			ids[i] = p.ID
		}(i)
	}
	wg.Wait()
	first := ids[0]
	for i, id := range ids {
		if id != first {
			t.Fatalf("request %d returned different payment %q != %q", i, id, first)
		}
	}
	a.mu.Lock()
	count := len(a.payments)
	a.mu.Unlock()
	if count != 1 {
		t.Fatalf("want 1 payment, got %d", count)
	}
}

func TestWebhookOnSuccess(t *testing.T) {
	const whSecret = "whsec-test"
	got := make(chan struct {
		Sig       string
		Timestamp string
		Body      []byte
	}, 1)
	merchant := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got <- struct {
			Sig       string
			Timestamp string
			Body      []byte
		}{r.Header.Get("X-ERupee-Signature"), r.Header.Get("X-ERupee-Timestamp"), body}
		w.WriteHeader(200)
	}))
	defer merchant.Close()

	a, err := newAPI(testMerchantKey, testSimulatorKey, merchant.URL, whSecret, &store{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.startWebhookWorker(ctx)
	h := a.handler()

	rec := doJSON(t, h, http.MethodPost, "/v1/payments", testMerchantKey, "wh1", `{"amount":2500,"currency":"INR","merchant_reference":"ORDER-WH"}`)
	var created struct{ ID string }
	json.Unmarshal(rec.Body.Bytes(), &created)
	confirmFlow(t, h, created.ID)

	select {
	case w := <-got:
		mac := hmac.New(sha256.New, []byte(whSecret))
		mac.Write([]byte(w.Timestamp + "." + string(w.Body)))
		want := hex.EncodeToString(mac.Sum(nil))
		if w.Sig != want {
			t.Fatalf("signature mismatch: got %q want %q", w.Sig, want)
		}
		if _, err := strconv.ParseInt(w.Timestamp, 10, 64); err != nil {
			t.Fatalf("bad timestamp %q: %v", w.Timestamp, err)
		}
		if time.Since(time.Unix(mustAtoi(w.Timestamp), 0)) > time.Minute {
			t.Fatal("timestamp too old")
		}
		var evt struct {
			ID   string `json:"id"`
			Type string `json:"type"`
			Data struct {
				PaymentID string `json:"payment_id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body, &evt); err != nil {
			t.Fatalf("bad event json: %v", err)
		}
		if evt.Type != "payment.succeeded" || evt.Data.PaymentID != created.ID || evt.ID == "" {
			t.Fatalf("bad event: %+v", evt)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("webhook never delivered")
	}
}

func mustAtoi(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

func TestExpiry(t *testing.T) {
	a := newTestAPI(t)
	h := a.handler()

	rec := doJSON(t, h, http.MethodPost, "/v1/payments", testMerchantKey, "exp1", `{"amount":700,"currency":"INR","merchant_reference":"ORDER-EXP"}`)
	var created struct{ ID string }
	json.Unmarshal(rec.Body.Bytes(), &created)

	a.mu.Lock()
	a.payments[created.ID].ExpiresAt = time.Now().UTC().Add(-time.Minute)
	a.mu.Unlock()

	got := doJSON(t, h, http.MethodGet, "/v1/payments/"+created.ID, testMerchantKey, "", "")
	var afterRead struct{ Status string }
	json.Unmarshal(got.Body.Bytes(), &afterRead)
	if afterRead.Status != statusExpired {
		t.Fatalf("want EXPIRED on read, got %q", afterRead.Status)
	}

	if rec := doJSON(t, h, http.MethodPost, "/v1/payments/"+created.ID+"/confirm", testSimulatorKey, "", ""); rec.Code != http.StatusConflict {
		t.Fatalf("confirm on expired: want 409, got %d", rec.Code)
	}
}

func succeedPayment(t *testing.T, h http.Handler, idem string) string {
	t.Helper()
	rec := doJSON(t, h, http.MethodPost, "/v1/payments", testMerchantKey, idem, `{"amount":2500,"currency":"INR","merchant_reference":"ORDER-RF"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: got %d", rec.Code)
	}
	var created struct{ ID string }
	json.Unmarshal(rec.Body.Bytes(), &created)
	confirmFlow(t, h, created.ID)
	return created.ID
}

func TestRefundHappyPath(t *testing.T) {
	h := newTestAPI(t).handler()
	payID := succeedPayment(t, h, "rf1")

	rec := doJSON(t, h, http.MethodPost, "/v1/payments/"+payID+"/refunds", testMerchantKey, "", `{"amount":2500,"reason":"order_cancelled"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("refund: got %d %s", rec.Code, rec.Body.String())
	}
	var rf struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Amount int64  `json:"amount"`
	}
	json.Unmarshal(rec.Body.Bytes(), &rf)
	if rf.Status != refunded || rf.Amount != 2500 {
		t.Fatalf("bad refund: %+v", rf)
	}

	got := doJSON(t, h, http.MethodGet, "/v1/refunds/"+rf.ID, testMerchantKey, "", "")
	if got.Code != http.StatusOK {
		t.Fatalf("get refund: got %d", got.Code)
	}
}

func TestRefundPartialThenExceedBlocked(t *testing.T) {
	h := newTestAPI(t).handler()
	payID := succeedPayment(t, h, "rf2")

	rec := doJSON(t, h, http.MethodPost, "/v1/payments/"+payID+"/refunds", testMerchantKey, "", `{"amount":1000,"reason":"partial"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("partial refund: got %d %s", rec.Code, rec.Body.String())
	}
	var rf struct {
		Status string `json:"status"`
	}
	json.Unmarshal(rec.Body.Bytes(), &rf)
	if rf.Status != refunded {
		t.Fatalf("partial refund status: want REFUNDED, got %q", rf.Status)
	}
	rec = doJSON(t, h, http.MethodPost, "/v1/payments/"+payID+"/refunds", testMerchantKey, "", `{"amount":1501,"reason":"over"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("over-refund: want 422, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestRefundGuards(t *testing.T) {
	h := newTestAPI(t).handler()
	payID := succeedPayment(t, h, "rf3")

	if rec := doJSON(t, h, http.MethodPost, "/v1/payments/"+payID+"/refunds", testSimulatorKey, "", `{"amount":100}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("simulator key must not refund: got %d", rec.Code)
	}
	if rec := doJSON(t, h, http.MethodPost, "/v1/payments/"+payID+"/refunds", testMerchantKey, "", `{"amount":0}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad amount: want 400, got %d", rec.Code)
	}
	if rec := doJSON(t, h, http.MethodPost, "/v1/payments/pay_er_nope/refunds", testMerchantKey, "", `{"amount":100}`); rec.Code != http.StatusNotFound {
		t.Fatalf("missing payment: want 404, got %d", rec.Code)
	}
	if rec := doJSON(t, h, http.MethodGet, "/v1/refunds/ref_nope", testMerchantKey, "", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("missing refund: want 404, got %d", rec.Code)
	}
}

func TestWebhookRetriesUntilSuccess(t *testing.T) {
	const whSecret = "whsec-retry"
	var mu sync.Mutex
	var hits int
	merchant := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		n := hits
		mu.Unlock()
		if n < 3 {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
	}))
	defer merchant.Close()

	a, err := newAPI(testMerchantKey, testSimulatorKey, merchant.URL, whSecret, &store{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.startWebhookWorker(ctx)
	h := a.handler()

	rec := doJSON(t, h, http.MethodPost, "/v1/payments", testMerchantKey, "retry1", `{"amount":500,"currency":"INR","merchant_reference":"ORDER-RETRY"}`)
	var created struct{ ID string }
	json.Unmarshal(rec.Body.Bytes(), &created)
	confirmFlow(t, h, created.ID)

	deadline := time.Now().Add(20 * time.Second)
	for {
		mu.Lock()
		n := hits
		mu.Unlock()
		if n >= 3 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("webhook not retried to success, hits=%d", n)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestDynamicQR(t *testing.T) {
	h := newTestAPI(t).handler()

	rec := doJSON(t, h, http.MethodPost, "/v1/qr", testMerchantKey, "", `{"amount":1250,"merchant_reference":"POS-91827"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d", rec.Code)
	}
	var qr struct {
		ID      string `json:"id"`
		Payload string `json:"payload"`
		Amount  int64  `json:"amount"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &qr); err != nil {
		t.Fatal(err)
	}
	if qr.ID == "" || !strings.HasPrefix(qr.ID, "qr_") {
		t.Fatalf("bad QR id %q", qr.ID)
	}
	if !strings.Contains(qr.Payload, "raumpay://cbdc/qr/") || !strings.Contains(qr.Payload, "amount=1250") {
		t.Fatalf("bad payload %q", qr.Payload)
	}
	if qr.Amount != 1250 {
		t.Fatalf("bad amount %d", qr.Amount)
	}

	if rec := doJSON(t, h, http.MethodPost, "/v1/qr", testSimulatorKey, "", `{"amount":100,"merchant_reference":"X"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("simulator key must not create QR: got %d", rec.Code)
	}
	if rec := doJSON(t, h, http.MethodPost, "/v1/qr", testMerchantKey, "", `{"amount":0,"merchant_reference":"X"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for bad amount, got %d", rec.Code)
	}
	if rec := doJSON(t, h, http.MethodPost, "/v1/qr", testMerchantKey, "", `{"amount":100,"merchant_reference":"X","rail":"UPI"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for UPI rail, got %d", rec.Code)
	}
}
