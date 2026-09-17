package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

const (
	testMerchantKey  = "merchant-test-key"
	testSimulatorKey = "simulator-test-key"
)

func newTestAPI(t *testing.T) *api {
	t.Helper()
	a, err := newAPI(testMerchantKey, testSimulatorKey)
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
