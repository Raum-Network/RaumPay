export type Payment = {
  id: string;
  status: string;
  amount: number;
  currency: string;
  provider: string;
  merchant_reference: string;
  completed_at: string;
};

const API = process.env.RAUMPAY_API_URL ?? "http://127.0.0.1:8080";
const KEY = process.env.RAUMPAY_MERCHANT_KEY ?? "";

async function call(path: string, init?: RequestInit): Promise<Response> {
  return fetch(`${API}${path}`, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${KEY}`,
      ...(init?.headers ?? {}),
    },
    cache: "no-store",
  });
}

export async function listPayments(): Promise<Payment[]> {
  const res = await call("/v1/payments");
  if (!res.ok) return [];
  const data = (await res.json()) as { payments: Payment[] };
  return data.payments;
}

export async function getPayment(id: string): Promise<Payment | null> {
  const res = await call(`/v1/payments/${id}`);
  if (!res.ok) return null;
  return (await res.json()) as Payment;
}

export async function createRefund(
  paymentId: string,
  amount: number,
  reason: string,
): Promise<{ ok: boolean; error?: string }> {
  const res = await call(`/v1/payments/${paymentId}/refunds`, {
    method: "POST",
    body: JSON.stringify({ amount, reason }),
  });
  if (res.ok) return { ok: true };
  const body = (await res.json().catch(() => ({}))) as { error?: string };
  return { ok: false, error: body.error ?? `HTTP ${res.status}` };
}

export async function createPayment(
  amount: number,
  merchantReference: string,
  idempotencyKey: string,
): Promise<{ ok: boolean; id?: string; error?: string }> {
  const res = await call("/v1/payments", {
    method: "POST",
    headers: { "Idempotency-Key": idempotencyKey },
    body: JSON.stringify({
      amount,
      currency: "INR",
      merchant_reference: merchantReference,
    }),
  });
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { error?: string };
    return { ok: false, error: body.error ?? `HTTP ${res.status}` };
  }
  const data = (await res.json()) as { id: string };
  return { ok: true, id: data.id };
}
