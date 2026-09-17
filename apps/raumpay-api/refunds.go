package main

import (
	"encoding/json"
	"io"
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

	a.mu.Lock()
	p := a.payments[r.PathValue("id")]
	if p == nil {
		a.mu.Unlock()
		httpError(w, http.StatusNotFound, "payment_not_found")
		return
	}
	a.expireIfDue(p)
	if p.Status != statusSucceeded {
		a.mu.Unlock()
		httpError(w, http.StatusConflict, "payment_not_refundable")
		return
	}
	refundedPaise := int64(0)
	for _, rf := range a.refunds {
		if rf.PaymentID == p.ID && rf.Status == refunded {
			refundedPaise += rf.AmountPaise
		}
	}
	if refundedPaise+req.Amount > p.AmountPaise {
		a.mu.Unlock()
		httpError(w, http.StatusUnprocessableEntity, "refund_exceeds_payment")
		return
	}

	rf := &refund{
		ID: a.newID("ref_"), PaymentID: p.ID, AmountPaise: req.Amount,
		Reason: req.Reason, Status: refundRequested,
		CreatedAt: time.Now().UTC(),
	}
	a.refunds[rf.ID] = rf
	a.mu.Unlock()

	// mock_cbdc: confirm immediately through the state machine.
	a.mu.Lock()
	if rf.Status == refundRequested {
		rf.Status = refundProcessing
		rf.Status = refunded
	}
	snapshot := *rf
	a.mu.Unlock()

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"id": snapshot.ID, "payment_id": snapshot.PaymentID,
		"status": snapshot.Status, "amount": snapshot.AmountPaise,
		"reason": snapshot.Reason,
	})
}

func (a *api) getRefund(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	rf := a.refunds[r.PathValue("id")]
	a.mu.Unlock()
	if rf == nil {
		httpError(w, http.StatusNotFound, "refund_not_found")
		return
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"id": rf.ID, "payment_id": rf.PaymentID,
		"status": rf.Status, "amount": rf.AmountPaise, "reason": rf.Reason,
	})
}
