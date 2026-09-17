import { getPayment } from "@/lib/api";
import CheckoutStatus from "@/components/checkout-status";

export const dynamic = "force-dynamic";

export default async function Checkout({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  const payment = await getPayment(id).catch(() => null);

  if (!payment) {
    return (
      <main style={{ padding: 32, fontFamily: "sans-serif", textAlign: "center" }}>
        <h1>Payment not found</h1>
        <p style={{ color: "#666" }}>This payment link is invalid or expired.</p>
      </main>
    );
  }

  const rupees = (payment.amount / 100).toFixed(2);

  return (
    <main style={{ padding: 32, fontFamily: "sans-serif", maxWidth: 480, margin: "0 auto" }}>
      <p style={{ color: "#666", marginBottom: 4 }}>Pay with Digital Rupee</p>
      <h1 style={{ margin: 0 }}>₹{rupees}</h1>
      <p style={{ color: "#666" }}>Provider: {payment.provider}</p>

      <div
        style={{
          border: "1px solid #ddd",
          borderRadius: 12,
          padding: 24,
          textAlign: "center",
          marginTop: 16,
        }}
      >
        <p style={{ fontWeight: 600, marginBottom: 8 }}>Scan with your e₹ wallet</p>
        <code
          style={{
            display: "block",
            background: "#f5f5f5",
            padding: 12,
            borderRadius: 8,
            fontSize: 12,
            wordBreak: "break-all",
          }}
        >
          raumpay://cbdc/qr/{payment.id}?amount={payment.amount}
        </code>
        <p style={{ color: "#888", fontSize: 12 }}>
          (QR image rendering arrives with the consumer app slice)
        </p>
      </div>

      <CheckoutStatus paymentId={payment.id} initialStatus={payment.status} />
    </main>
  );
}
