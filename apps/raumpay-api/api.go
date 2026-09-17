package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	statusCreated        = "CREATED"
	statusRequiresAction = "REQUIRES_ACTION"
	statusProcessing     = "PROCESSING"
	statusSucceeded      = "SUCCEEDED"
	statusFailed         = "FAILED"
	statusExpired        = "EXPIRED"
)

const maxBodyBytes = 64 << 10

type api struct {
	mu       sync.Mutex
	payments map[string]*payment
	idem     map[string]idemRecord
	qrs      map[string]*qrCode
	merchantKey,
	simulatorKey []byte
	webhookURL    string
	webhookSecret []byte
}

type idemRecord struct {
	paymentID string
	bodyHash  [sha256.Size]byte
}

type payment struct {
	ID, Status, Currency, Provider, MerchantRef string
	AmountPaise                                 int64
	CreatedAt, ExpiresAt, CompletedAt           time.Time
}

type createReq struct {
	AmountPaise int64  `json:"amount"`
	Currency    string `json:"currency"`
	MerchantRef string `json:"merchant_reference"`
}

func newAPI(merchantKey, simulatorKey, webhookURL, webhookSecret string) (*api, error) {
	if merchantKey == "" || simulatorKey == "" {
		return nil, errors.New("RAUMPAY_MERCHANT_KEY and RAUMPAY_SIMULATOR_KEY must be set (e.g. openssl rand -hex 32)")
	}
	if webhookURL != "" && webhookSecret == "" {
		return nil, errors.New("RAUMPAY_WEBHOOK_SECRET is required when RAUMPAY_WEBHOOK_URL is set")
	}
	return &api{
		payments:      map[string]*payment{},
		idem:          map[string]idemRecord{},
		qrs:           map[string]*qrCode{},
		merchantKey:   []byte(merchantKey),
		simulatorKey:  []byte(simulatorKey),
		webhookURL:    webhookURL,
		webhookSecret: []byte(webhookSecret),
	}, nil
}

func (a *api) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/payments", a.requireKey(a.merchantKey, a.createPayment))
	mux.HandleFunc("GET /v1/payments/{id}", a.requireKey(a.merchantKey, a.getPayment))
	mux.HandleFunc("POST /v1/payments/{id}/confirm", a.requireKey(a.simulatorKey, a.confirmPayment))
	mux.HandleFunc("POST /v1/qr", a.requireKey(a.merchantKey, a.createDynamicQR))
	return a.securityHeaders(mux)
}

func (a *api) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "no-store")
		h.Set("Content-Type", "application/json")
		next.ServeHTTP(w, r)
	})
}

type keyFunc func(http.ResponseWriter, *http.Request)

func (a *api) requireKey(want []byte, next keyFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(auth, prefix) ||
			subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(auth, prefix)), want) != 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		next(w, r)
	}
}

func (a *api) createPayment(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid_request_body")
		return
	}
	var req createReq
	if err := json.Unmarshal(body, &req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if req.AmountPaise <= 0 || req.Currency != "INR" || strings.TrimSpace(req.MerchantRef) == "" {
		httpError(w, http.StatusBadRequest, "validation_failed")
		return
	}
	idem := r.Header.Get("Idempotency-Key")
	if idem == "" {
		httpError(w, http.StatusBadRequest, "idempotency_key_required")
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if prev, ok := a.idem[idem]; ok {
		if prev.bodyHash != sha256.Sum256(body) {
			httpError(w, http.StatusUnprocessableEntity, "idempotency_key_reused")
			return
		}
		a.writePayment(w, prev.paymentID, http.StatusOK)
		return
	}
	now := time.Now().UTC()
	p := &payment{
		ID: a.newID("pay_er_"), Status: statusCreated,
		AmountPaise: req.AmountPaise, Currency: req.Currency,
		Provider: "mock_cbdc", MerchantRef: req.MerchantRef,
		CreatedAt: now, ExpiresAt: now.Add(15 * time.Minute),
	}
	a.payments[p.ID] = p
	a.idem[idem] = idemRecord{paymentID: p.ID, bodyHash: sha256.Sum256(body)}
	a.writePayment(w, p.ID, http.StatusCreated)
}

func (a *api) getPayment(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.writePayment(w, r.PathValue("id"), http.StatusOK)
}

func (a *api) confirmPayment(w http.ResponseWriter, r *http.Request) {
	// Simulator-only: moves a payment toward a final state.
	a.mu.Lock()
	defer a.mu.Unlock()
	id := r.PathValue("id")
	p := a.payments[id]
	if p == nil {
		httpError(w, http.StatusNotFound, "payment_not_found")
		return
	}
	switch p.Status {
	case statusCreated:
		p.Status = statusRequiresAction
	case statusRequiresAction:
		p.Status = statusProcessing
	case statusProcessing:
		p.Status = statusSucceeded
	default:
		httpError(w, http.StatusConflict, "invalid_state")
		return
	}
	if p.Status == statusSucceeded {
		p.CompletedAt = time.Now().UTC()
		snapshot := *p
		a.mu.Unlock()
		a.deliverWebhook(snapshot)
		a.mu.Lock()
	}
	a.writePayment(w, id, http.StatusOK)
}

func (a *api) writePayment(w http.ResponseWriter, id string, code int) {
	p := a.payments[id]
	if p == nil {
		httpError(w, http.StatusNotFound, "payment_not_found")
		return
	}
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]any{
		"id": p.ID, "status": p.Status, "amount": p.AmountPaise,
		"currency": p.Currency, "provider": p.Provider,
		"merchant_reference": p.MerchantRef, "completed_at": p.CompletedAt,
	})
}

func (a *api) newID(prefix string) string {
	b := make([]byte, 12)
	rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

func httpError(w http.ResponseWriter, code int, msg string) {
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
