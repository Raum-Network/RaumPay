package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type qrReq struct {
	AmountPaise int64  `json:"amount"`
	MerchantRef string `json:"merchant_reference"`
	QRType      string `json:"type"`
	Rail        string `json:"rail"`
}

func (a *api) createDynamicQR(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid_request_body")
		return
	}
	var req qrReq
	if err := json.Unmarshal(body, &req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if req.AmountPaise <= 0 || strings.TrimSpace(req.MerchantRef) == "" {
		httpError(w, http.StatusBadRequest, "validation_failed")
		return
	}
	if req.QRType != "" && req.QRType != "dynamic" {
		httpError(w, http.StatusBadRequest, "only dynamic QR supported")
		return
	}
	if req.Rail != "" && req.Rail != "CBDC" {
		httpError(w, http.StatusBadRequest, "only CBDC rail supported")
		return
	}

	qrID := a.newID("qr_")
	now := time.Now().UTC()
	expiresAt := now.Add(15 * time.Minute)

	// ponytail: raw CBDC payload string only, no PNG/SVG rendering — checkout UI
	// or a JS lib renders it; add image generation when hosted checkout needs it.
	payload := "raumpay://cbdc/qr/" + qrID + "?amount=" + strconv.FormatInt(req.AmountPaise, 10)

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"id":         qrID,
		"payload":    payload,
		"amount":     req.AmountPaise,
		"expires_at": expiresAt.Format(time.RFC3339),
		"status":     "ACTIVE",
	})
}
