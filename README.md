# RaumPay

**A sandbox-first merchant payment platform being developed by Raum Network for India's digital rupee (e₹ / CBDC) ecosystem.**

RaumPay aims to give businesses a single integration for initiating digital-rupee payments, presenting checkout experiences, tracking payment status, receiving payment notifications, and managing refunds. Its intended role is the merchant-facing software layer between business applications and authorized payment providers—not a currency issuer, a bank, or a consumer wallet.

> **Current status: local proof of concept.** All payments and refunds use the `mock_cbdc` simulator. No real money moves, no live bank or wallet integration is implemented, and this repository does not establish regulatory approval or certification. The API refuses to start unless `RAUMPAY_MODE=sandbox` and listens only on `127.0.0.1:8080`.

## What RaumPay will do

The intended product will help merchants and developers:

- **Accept digital-rupee payments:** create payment requests from an online store, business application, or point-of-sale integration.
- **Offer a checkout experience:** show an amount and payment instructions, eventually including provider-compatible QR images and wallet handoff.
- **Track transactions:** associate payments with merchant order references and expose their lifecycle through an API and dashboard.
- **Automate order updates:** notify merchant backends of confirmed payments using authenticated webhook events.
- **Manage refunds:** initiate full or partial refunds and track provider-confirmed outcomes.
- **Integrate through a consistent API:** keep merchant-facing contracts separate from future bank/provider-specific adapters.
- **Operate reliably:** evolve toward durable transaction records, reconciliation, auditability, and operational monitoring.

These are product goals, not a claim that all capabilities are production-ready. Live integrations depend on provider access, commercial arrangements, security review, and applicable legal and regulatory requirements.

## What exists today

| Area | Implemented prototype | Important boundary |
| --- | --- | --- |
| Payment API | Create, list, and retrieve INR payments; merchant references; amounts in integer paise | One configured merchant credential; no multi-tenant account model |
| Payment lifecycle | Simulator-driven transitions and 15-minute expiry checked on access | No bank confirmation or real settlement |
| Idempotency | Required key for payment creation; identical request bodies reuse the payment | In-memory lookup; no restart-safe guarantees |
| Dynamic QR payloads | Creates a QR identifier, custom URI payload, amount, and expiry | No QR image rendering or verified wallet interoperability |
| Refunds | Full/partial mock refunds for successful payments; atomic, overflow-safe over-refund validation | Immediate simulated success, not a provider reversal |
| Webhooks | Signed `payment.succeeded` events; background delivery with backoff, jitter, and a five-attempt limit | In-memory queue; no durable delivery log or replay |
| Merchant dashboard | Next.js payment listing/detail pages, create form, and refund action; server action keeps the merchant key server-side | UI scaffold; merchant authentication is not implemented |
| Hosted checkout | Amount and placeholder URI display; same-origin status polling proxy | Not a working wallet checkout |
| PostgreSQL | Optional schema creation and best-effort payment/idempotency writes | Reads remain in memory; persisted state is not restored on restart |
| Infrastructure | OCI Terraform proof-of-concept configuration | Not a complete deployment, cost guarantee, or production security baseline |

## Intended payment journey

1. A merchant's backend creates a payment with an amount and order reference.
2. RaumPay returns a payment ID, which the merchant uses to track the transaction.
3. The payer is shown a checkout experience and, in a future provider integration, a compatible QR or wallet handoff.
4. The provider confirms the payment; RaumPay records the outcome and notifies the merchant.
5. The merchant updates the order after verifying the payment status or signed webhook.
6. If needed, the merchant requests a refund and tracks its outcome.

**In the current sandbox, step 4 is performed manually through the simulator-only confirmation endpoint.** The QR endpoint returns a standalone payload without retaining a QR record; it does not link a wallet payment to a payment record. The checkout page's URI is also a placeholder, not a bank-issued payment instruction.

## Architecture

```text
Merchant application / local dashboard
                 |
                 v
         Go HTTP API (localhost)
            |          |
            |          +--> in-memory webhook queue --> merchant receiver
            |
            +--> in-memory payment, refund, and idempotency maps
            |
            +--> optional PostgreSQL best-effort writes
            |
            +--> mock_cbdc simulator (no external payment rail)
```

- **Backend:** Go 1.26, standard-library `net/http`, PostgreSQL access through `pgx`.
- **Frontend:** Next.js 16, React 19, TypeScript.
- **Infrastructure:** Terraform and the Oracle Cloud Infrastructure provider.
- **Authentication:** bearer tokens with separate merchant and simulator roles.
- **Webhook signatures:** HMAC-SHA256 over the timestamp and original request body.

## Repository layout

```text
apps/
  raumpay-api/
    main.go          Sandbox startup, HTTP server, shutdown
    api.go           Payment endpoints, authentication, idempotency, expiry
    qr.go            Dynamic placeholder QR payloads
    refunds.go       Simulated refund handling
    webhooks.go      Signed events and delivery worker
    store.go         Optional PostgreSQL schema and write helpers
    api_test.go      API and workflow tests
  merchant-dashboard/
    app/             Merchant and checkout pages
    components/      Payment form and checkout status UI
    lib/api.ts       Backend API helpers
    package.json     Frontend dependencies and scripts
deploy/
  terraform/         OCI infrastructure prototype
```

## Run the API locally

### Prerequisites

- Go 1.26 or a compatible newer toolchain.
- Git, `curl`, and OpenSSL for the examples below.
- Node.js compatible with the bundled Next.js version and npm, only for the dashboard.
- PostgreSQL, only if testing the optional database write path.

```bash
git clone https://github.com/Raum-Network/RaumPay.git
cd RaumPay/apps/raumpay-api

export RAUMPAY_MODE=sandbox
export RAUMPAY_MERCHANT_KEY="$(openssl rand -hex 32)"
export RAUMPAY_SIMULATOR_KEY="$(openssl rand -hex 32)"

go run .
```

Keep the keys distinct. For API requests in another terminal, set that terminal's variables to the same values used by the running server. Do not commit credentials or put the simulator key in browser code.

### Configuration

| Variable | Required | Purpose |
| --- | --- | --- |
| `RAUMPAY_MODE` | Yes | Must be `sandbox` |
| `RAUMPAY_MERCHANT_KEY` | Yes | Bearer token for merchant endpoints |
| `RAUMPAY_SIMULATOR_KEY` | Yes | Separate bearer token for simulated confirmation |
| `RAUMPAY_WEBHOOK_URL` | No | Trusted merchant webhook receiver URL |
| `RAUMPAY_WEBHOOK_SECRET` | When webhook URL is set | Shared secret for webhook verification |
| `RAUMPAY_DATABASE_URL` | No | PostgreSQL connection string; unset uses memory only |
| `RAUMPAY_API_URL` | Dashboard only | Backend URL; defaults to `http://127.0.0.1:8080` |

The database schema is created at startup when a database URL is configured. This is not complete durable storage: reads, recovery, transactional guarantees, QR/refund persistence wiring, and webhook persistence remain unfinished. Restarting loses the API's usable in-memory state even when PostgreSQL is configured.

## API reference

Base URL: `http://127.0.0.1:8080`. Bodies and responses are JSON.

| Method | Path | Authentication | Purpose |
| --- | --- | --- | --- |
| `POST` | `/v1/payments` | Merchant | Create payment; requires `Idempotency-Key` |
| `GET` | `/v1/payments` | Merchant | List payments held in memory |
| `GET` | `/v1/payments/{id}` | Merchant | Retrieve payment and check expiry |
| `POST` | `/v1/payments/{id}/confirm` | Simulator | Advance one simulated lifecycle step |
| `POST` | `/v1/qr` | Merchant | Create a dynamic mock CBDC QR payload |
| `GET` | `/v1/checkout/{id}` | None | Minimal public payment ID/status projection |
| `POST` | `/v1/payments/{id}/refunds` | Merchant | Request a simulated refund |
| `GET` | `/v1/refunds/{id}` | Merchant | Retrieve refund details |

### Create a payment

All API amounts are **integer paise**: `10000` means ₹100.00. Currency must be `INR`; the merchant reference must be nonblank.

```bash
curl --fail-with-body http://127.0.0.1:8080/v1/payments \
  -H "Authorization: Bearer $RAUMPAY_MERCHANT_KEY" \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: demo-order-001' \
  --data '{"amount":10000,"currency":"INR","merchant_reference":"order-001"}'
```

The response includes `id`, `status`, `amount`, `currency`, `provider`, and `merchant_reference`. Reusing the idempotency key with the exact same request body returns the existing payment; using different body bytes returns `422 idempotency_key_reused`.

### Simulate a successful payment

Copy the returned ID into `PAYMENT_ID`. Each confirmation advances exactly one step, so a new payment needs three calls:

```bash
export PAYMENT_ID='replace-with-returned-payment-id'
for step in 1 2 3; do
  curl --fail-with-body -X POST \
    "http://127.0.0.1:8080/v1/payments/$PAYMENT_ID/confirm" \
    -H "Authorization: Bearer $RAUMPAY_SIMULATOR_KEY"
  printf '\n'
done
```

```text
CREATED -> REQUIRES_ACTION -> PROCESSING -> SUCCEEDED
```

Non-final payments become `EXPIRED` when checked after their 15-minute deadline. The code defines `FAILED`, but the confirmation endpoint only simulates the success path. Further confirmation of a terminal payment is rejected.

### Request a partial refund

```bash
curl --fail-with-body \
  "http://127.0.0.1:8080/v1/payments/$PAYMENT_ID/refunds" \
  -H "Authorization: Bearer $RAUMPAY_MERCHANT_KEY" \
  -H 'Content-Type: application/json' \
  --data '{"amount":2500,"reason":"Partial return"}'
```

Only successful payments are eligible. Refund creation requires an `Idempotency-Key`; identical replays return the original refund, and concurrent reservations cannot exceed the refundable amount. The mock handler creates refunds directly as `REFUNDED`. This does not transfer funds.

### Create a mock QR payload

```bash
curl --fail-with-body http://127.0.0.1:8080/v1/qr \
  -H "Authorization: Bearer $RAUMPAY_MERCHANT_KEY" \
  -H 'Content-Type: application/json' \
  --data '{"amount":10000,"merchant_reference":"order-001","type":"dynamic","rail":"CBDC"}'
```

The result contains a `raumpay://cbdc/qr/...` payload, not an image. Do not expect a real e₹ wallet to recognize it.

## Webhook integration

When configured, the worker sends `payment.succeeded` events containing an event ID, creation timestamp, and payment ID, merchant reference, and amount.

| Header | Meaning |
| --- | --- |
| `X-ERupee-Event-ID` | Stable event identifier for deduplication |
| `X-ERupee-Timestamp` | Unix timestamp for this delivery attempt |
| `X-ERupee-Signature` | Hex-encoded HMAC-SHA256 signature |

To verify an event, compute HMAC-SHA256 with the shared secret over `timestamp + "." + raw_request_body` and compare signatures in constant time. Reject stale timestamps using an appropriate replay window and deduplicate event IDs before applying business effects. Return a 2xx response after accepting the event.

The queue uses exponential backoff and jitter and enforces a five-attempt limit. Exhausted jobs are no longer dispatched. Delivery state is not durable and there is no replay/dead-letter interface. Treat webhook delivery as experimental.

## Dashboard development

From a second terminal, using the same merchant key as the backend:

```bash
cd RaumPay/apps/merchant-dashboard
export RAUMPAY_API_URL=http://127.0.0.1:8080
export RAUMPAY_MERCHANT_KEY='same-value-as-the-running-api'
npm ci
npm run dev -- --hostname 127.0.0.1
```

Open `http://127.0.0.1:3000`. Pages include the payment list, `/payments/{id}`, and `/checkout/{id}`.

Payment creation runs through a server action; the API client is server-only and merchant credentials remain on the server. Checkout polls the same-origin `/checkout/api/{id}` route. **Known integration gaps:** merchant login and authorization are absent. Keep the dashboard bound to localhost; do not expose it publicly or publish merchant credentials to the browser.

## Validation

```bash
cd apps/raumpay-api
go test ./...
go vet ./...
```

The Go suite covers authentication, validation, idempotent replay and concurrent creation, the simulated success flow, signed webhooks, retry-to-success and retry exhaustion, checkout status, expiry, concurrent and overflow-safe refund guards, and QR validation. Passing tests do not establish production readiness or real-provider compatibility.

The frontend provides `npm run build`; it does not currently define a test or lint script.

## Infrastructure status

`deploy/terraform/main.tf` describes an OCI proof-of-concept environment using an A1 Flex VM, networking, and a boot volume, with configuration intended to constrain the deployment to a free-tier profile. It does not install the application, database, tunnel agent, or a complete operations stack.

Review Terraform plans, account quotas, security lists, credentials, regional capacity, and current provider pricing before provisioning. A resource shape or a guard variable alone cannot guarantee a zero bill or secure isolation. No infrastructure is deployed merely by cloning this repository.

## Roadmap

These are proposed next steps, not delivery commitments:

1. **Complete the local demo:** finish QR/payment association and verify the browser journey. Dashboard server boundaries, checkout polling, retry exhaustion, and atomic refund limits are implemented.
2. **Make storage authoritative:** database-backed reads, transactional writes, durable idempotency, refund reservations, migrations, and restart recovery.
3. **Make event delivery reliable:** durable outbox, bounded retries, delivery logs, replay tools, and operational alerts.
4. **Integrate authorized providers:** replace the mock adapter with provider-approved payment, status, QR, and refund flows and validate them in provider sandboxes.
5. **Support merchant operations:** onboarding, tenant isolation, scoped/rotatable credentials, role-based access, searchable history, and reconciliation.
6. **Prepare for production:** threat modeling, security review, rate limiting, audit trails, monitoring, backup/recovery testing, load testing, and applicable compliance review.

Live payment acceptance, wallet interoperability, settlement, reconciliation, production availability, and regulatory approval must not be inferred from this proof of concept.

## Contributing

Keep changes focused, add or update tests for backend behavior, and clearly distinguish simulated features from real-provider integrations. Never commit API keys, database credentials, private keys, Terraform state, or local environment files.

No license has been selected in this repository. Public visibility does not by itself grant an open-source license.
