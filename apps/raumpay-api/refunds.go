package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"
)

const (
	refundRequested  = "REFUND_REQUESTED"
	refundProcessing = "REFUND_PROCESSING"
	refunded         = "REFUNDED"
	refundFailed     = "REFUND_FAILED"
)

type refund struct {
	ID, PaymentID, Status, Reason string
	AmountPaise                   int64
	CreatedAt, CompletedAt        time.Time
}

// ponytail: refund state machine mirrors Plan-2 §16 (SUCCEEDED →
// REFUND_REQUESTED → REFUND_PROCESSING → REFUNDED|REFUND_FAILED); no
// provider-side reversal call yet since only mock_cbdc exists. Wire the real
// adapter call into transitionRefund when the HDFC adapter lands.
func (a *api) createRefund(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid_request_body")
		return
	}
	var req struct {
		Amount int64  `json:"amount"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if req.Amount <= 0 {
		httpError(w, http.StatusBadRequest, "validation_failed")
		return
	}
	idem := r.Header.Get("Idempotency-Key")
	if idem == "" {
		httpError(w, http.StatusBadRequest, "idempotency_key_required")
		return
	}
	bodyHash := sha256.Sum256(body)

	a.mu.Lock()
	defer a.mu.Unlock()
	if prev, ok := a.idem[idem]; ok {
		if prev.bodyHash != bodyHash || prev.refundID == "" {
			httpError(w, http.StatusUnprocessableEntity, "idempotency_key_reused")
			return
		}
		rf := a.refunds[prev.refundID]
		if rf == nil {
			httpError(w, http.StatusNotFound, "refund_not_found")
			return
		}
		a.writeRefund(w, rf, http.StatusOK)
		return
	}
	p := a.payments[r.PathValue("id")]
	if p == nil {
		httpError(w, http.StatusNotFound, "payment_not_found")
		return
	}
	a.expireIfDue(p)
	if p.Status != statusSucceeded {
		httpError(w, http.StatusConflict, "payment_not_refundable")
		return
	}
	refundedPaise := int64(0)
	for _, rf := range a.refunds {
		if rf.PaymentID == p.ID && rf.Status == refunded {
			refundedPaise += rf.AmountPaise
		}
	}
	if req.Amount > p.AmountPaise-refundedPaise {
		httpError(w, http.StatusUnprocessableEntity, "refund_exceeds_payment")
		return
	}

	rf := &refund{
		ID: a.newID("ref_"), PaymentID: p.ID, AmountPaise: req.Amount,
		Reason: req.Reason, Status: refundRequested,
		CreatedAt: time.Now().UTC(),
	}
	a.refunds[rf.ID] = rf
	record := idemRecord{refundID: rf.ID, bodyHash: bodyHash}
	a.idem[idem] = record

	// mock_cbdc: confirm immediately through the state machine.
	if rf.Status == refundRequested {
		rf.Status = refundProcessing
		rf.Status = refunded
		rf.CompletedAt = time.Now().UTC()
		snapshot := *rf
		a.persist(func() {
			bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := a.st.saveRefund(bgCtx, &snapshot); err != nil {
				slog.Error("persist refund failed", "refund_id", snapshot.ID, "error", err)
			}
			if err := a.st.saveIdem(bgCtx, idem, record); err != nil {
				slog.Error("persist refund idempotency failed", "key", idem, "error", err)
			}
		})
	}
	snapshot := *rf
	a.writeRefund(w, &snapshot, http.StatusCreated)
}

func (a *api) getRefund(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	rf := a.refunds[r.PathValue("id")]
	a.mu.Unlock()
	if rf == nil {
		httpError(w, http.StatusNotFound, "refund_not_found")
		return
	}
	a.writeRefund(w, rf, http.StatusOK)
}

func (a *api) writeRefund(w http.ResponseWriter, rf *refund, code int) {
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]any{
		"id": rf.ID, "payment_id": rf.PaymentID,
		"status": rf.Status, "amount": rf.AmountPaise, "reason": rf.Reason,
	})
}
