import { getPayment, createRefund } from "@/lib/api";
import { redirect } from "next/navigation";

export const dynamic = "force-dynamic";

function fmt(paise: number): string {
  return `₹${(paise / 100).toFixed(2)}`;
}

export default async function PaymentDetail({
  params,
  searchParams,
}: {
  params: Promise<{ id: string }>;
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const { id } = await params;
  const sp = await searchParams;
  const payment = await getPayment(id);
  if (!payment) {
    return <main style={{ padding: 24 }}><h1>Payment not found</h1></main>;
  }

  let refundError: string | null = null;
  if (sp.refund === "failed") {
    refundError = typeof sp.error === "string" ? sp.error : "Refund failed";
  }

  async function refund(formData: FormData) {
    "use server";
    const amount = Number(formData.get("amount"));
    const reason = String(formData.get("reason") ?? "merchant_refund");
    if (!Number.isFinite(amount) || amount <= 0) {
      redirect(`/payments/${id}?refund=failed&error=invalid_amount`);
    }
    const res = await createRefund(
      id,
      Math.round(amount * 100),
      reason || "merchant_refund",
    );
    if (!res.ok) {
      redirect(`/payments/${id}?refund=failed&error=${encodeURIComponent(res.error ?? "failed")}`);
    }
    redirect(`/payments/${id}`);
  }

  const refundable = payment.status === "SUCCEEDED";

  return (
    <main style={{ padding: 24, fontFamily: "sans-serif", maxWidth: 640 }}>
      <p><a href="/">← All payments</a></p>
      <h1>{payment.id}</h1>
      <table style={{ borderCollapse: "collapse", marginTop: 16 }}>
        <tbody>
          <tr><td style={td}><strong>Status</strong></td><td style={td}>{payment.status}</td></tr>
          <tr><td style={td}><strong>Amount</strong></td><td style={td}>{fmt(payment.amount)}</td></tr>
          <tr><td style={td}><strong>Order</strong></td><td style={td}>{payment.merchant_reference}</td></tr>
          <tr><td style={td}><strong>Provider</strong></td><td style={td}>{payment.provider}</td></tr>
          <tr><td style={td}><strong>Completed</strong></td><td style={td}>{payment.completed_at || "—"}</td></tr>
        </tbody>
      </table>

      {refundError && <p style={{ color: "#c62828" }}>Refund failed: {refundError}</p>}

      {refundable ? (
        <form action={refund} style={{ marginTop: 24 }}>
          <h3>Refund</h3>
          <input
            name="amount"
            placeholder="Amount ₹"
            inputMode="decimal"
            required
            style={{ padding: 8, marginRight: 8, border: "1px solid #ccc", borderRadius: 6 }}
          />
          <input
            name="reason"
            placeholder="Reason (optional)"
            style={{ padding: 8, marginRight: 8, border: "1px solid #ccc", borderRadius: 6 }}
          />
          <button
            type="submit"
            style={{ padding: "8px 14px", borderRadius: 6, border: "none", background: "#1a1a1a", color: "white", cursor: "pointer" }}
          >
            Refund
          </button>
        </form>
      ) : (
        <p style={{ marginTop: 24, color: "#666" }}>
          Refunds are available only for SUCCEEDED payments.
        </p>
      )}
    </main>
  );
}

const td = { padding: "4px 16px 4px 0" } as const;
