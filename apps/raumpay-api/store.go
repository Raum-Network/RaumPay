package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// store abstracts persistence. In-memory default (sandbox); Postgres when
// RAUMPAY_DATABASE_URL is set. All methods run inside the api mutex in the
// in-memory case, so no extra locking here beyond pgxpool's own safety.
type store struct {
	pool *pgxpool.Pool
}

func newStore(ctx context.Context) (*store, func()) {
	dsn := os.Getenv("RAUMPAY_DATABASE_URL")
	if dsn == "" {
		slog.Warn("no RAUMPAY_DATABASE_URL: in-memory store, state lost on restart")
		return &store{}, func() {}
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		slog.Error("bad RAUMPAY_DATABASE_URL", "error", err)
		os.Exit(1)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		slog.Error("postgres connect failed", "error", err)
		os.Exit(1)
	}
	if err := migrate(ctx, pool); err != nil {
		slog.Error("migrate failed", "error", err)
		os.Exit(1)
	}
	slog.Info("postgres store enabled")
	return &store{pool: pool}, pool.Close
}

func migrate(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS payments (
	id TEXT PRIMARY KEY,
	status TEXT NOT NULL,
	amount_paise BIGINT NOT NULL,
	currency TEXT NOT NULL,
	provider TEXT NOT NULL,
	merchant_reference TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL,
	expires_at TIMESTAMPTZ NOT NULL,
	completed_at TIMESTAMPTZ
);
CREATE TABLE IF NOT EXISTS refunds (
	id TEXT PRIMARY KEY,
	payment_id TEXT NOT NULL REFERENCES payments(id),
	status TEXT NOT NULL,
	amount_paise BIGINT NOT NULL,
	reason TEXT,
	created_at TIMESTAMPTZ NOT NULL
);
CREATE TABLE IF NOT EXISTS idempotency_keys (
	key TEXT PRIMARY KEY,
	payment_id TEXT NOT NULL,
	body_hash BYTEA NOT NULL,
	created_at TIMESTAMPTZ NOT NULL
);`)
	return err
}

func (s *store) savePayment(ctx context.Context, p *payment) error {
	if s.pool == nil {
		return nil
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO payments
		(id, status, amount_paise, currency, provider, merchant_reference, created_at, expires_at, completed_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (id) DO UPDATE SET status=$2, completed_at=$9`,
		p.ID, p.Status, p.AmountPaise, p.Currency, p.Provider, p.MerchantRef,
		p.CreatedAt, p.ExpiresAt, nullableTime(p.CompletedAt))
	return err
}

func (s *store) saveRefund(ctx context.Context, rf *refund) error {
	if s.pool == nil {
		return nil
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO refunds
		(id, payment_id, status, amount_paise, reason, created_at)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (id) DO UPDATE SET status=$3`,
		rf.ID, rf.PaymentID, rf.Status, rf.AmountPaise, rf.Reason, rf.CreatedAt)
	return err
}

func (s *store) saveIdem(ctx context.Context, key string, rec idemRecord) error {
	if s.pool == nil {
		return nil
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO idempotency_keys (key, payment_id, body_hash, created_at)
		VALUES ($1,$2,$3,$4) ON CONFLICT (key) DO NOTHING`,
		key, rec.paymentID, rec.bodyHash[:], time.Now().UTC())
	return err
}

func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
