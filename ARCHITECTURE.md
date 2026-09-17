# RaumPay architecture

Current sandbox implementation and infrastructure configuration. This is not a production payment system; no live bank, wallet, or CBDC network integration is implemented.

```mermaid
flowchart TB
    Merchant["Merchant"]
    Payer["Payer"]
    Simulator["Sandbox operator / simulator"]

    subgraph App["Implemented application — local sandbox"]
        subgraph Web["Next.js 16 / React 19"]
            Dashboard["Merchant dashboard<br/>List, create, inspect payments; request refunds"]
            Checkout["Hosted checkout<br/>QR payload text and status polling"]
            Server["Server components / server actions<br/>Server-side merchant API key"]
            Proxy["Checkout status proxy<br/>/checkout/api/:id"]
        end

        subgraph Backend["Go HTTP API — 127.0.0.1:8080"]
            API["Merchant-key authenticated endpoints<br/>Payments, dynamic QR, refunds"]
            Public["Public checkout endpoint<br/>Payment ID and status only"]
            Confirm["Simulator-key authenticated confirmation"]
            Core["Sandbox payment and refund state machines<br/>mock_cbdc provider; idempotency; expiry"]
            Memory[("In-memory state<br/>Payments, refunds, idempotency, QR records")]
            Worker["In-process webhook queue and worker<br/>Payment success events; retries"]
        end
        DB[("Optional PostgreSQL<br/>Best-effort asynchronous writes<br/>No startup read-back")]
    end

    Receiver["Configured merchant webhook receiver"]

    Merchant --> Dashboard
    Payer --> Checkout
    Dashboard --> Server
    Checkout -->|"Initial payment lookup"| Server
    Server -->|"Bearer merchant key"| API
    Checkout -->|"Browser polls same-origin route"| Proxy
    Proxy --> Public
    Simulator -->|"Bearer simulator key"| Confirm
    API --> Core
    Confirm --> Core
    Core <--> Memory
    Public --> Memory
    Core -.->|"Optional persistence"| DB
    Core -->|"Payment succeeds"| Worker
    Worker -->|"HTTP POST; HMAC-SHA256 signature"| Receiver

    subgraph Infra["Deployment configuration — not verified as deployed"]
        Terraform["Terraform OCI resources"]
        Network["VCN + public subnet + internet gateway<br/>Route table and NSG"]
        VM["Ubuntu 24.04 A1 Flex VM<br/>2 OCPU / 12 GB RAM / 50 GB boot volume"]
        Tunnel["Planned Cloudflare Tunnel / cloudflared<br/>Not provisioned in current Terraform"]
        Terraform --> Network
        Network --> VM
        Tunnel -.->|"Intended application ingress"| VM
        VM -.->|"Intended host; app installation not provisioned"| App
    end
```

## Scope and caveats

- Solid application arrows show implemented call paths. Dashed arrows indicate optional persistence or intended deployment relationships.
- The API requires `RAUMPAY_MODE=sandbox`. Payment confirmation is simulated; checkout displays a payload string, not a rendered QR image or a working wallet integration.
- In-memory maps are the read path even when `RAUMPAY_DATABASE_URL` enables PostgreSQL writes. Persisted state is not restored at startup; the webhook queue is also in memory.
- Terraform defines OCI infrastructure, not application deployment or Cloudflare configuration. Despite its tunnel-only comments, its NSG currently allows SSH from `0.0.0.0/0`; do not interpret this diagram as evidence of a closed management surface.
- The merchant API key is used server-side by Next.js. Merchant dashboard user authentication is not shown because it is not implemented in the inspected application.

## Source map

- `apps/merchant-dashboard/app/` — dashboard, payment details, hosted checkout, and polling proxy.
- `apps/merchant-dashboard/lib/api.ts` — server-side API client.
- `apps/raumpay-api/main.go` and `api.go` — sandbox startup, routes, payment state, and confirmation.
- `apps/raumpay-api/qr.go`, `refunds.go`, and `webhooks.go` — QR payloads, refunds, and webhook delivery.
- `apps/raumpay-api/store.go` — optional PostgreSQL writes.
- `deploy/terraform/main.tf` — OCI network and compute configuration.
