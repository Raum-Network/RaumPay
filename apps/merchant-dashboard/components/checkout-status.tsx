"use client";

import { useEffect, useState } from "react";

const LABELS: Record<string, string> = {
  CREATED: "Waiting for payment…",
  REQUIRES_ACTION: "Waiting for payment…",
  PROCESSING: "Processing payment…",
  SUCCEEDED: "Payment successful ✓",
  FAILED: "Payment failed",
  EXPIRED: "Payment link expired",
};

export default function CheckoutStatus({
  paymentId,
  initialStatus,
}: {
  paymentId: string;
  initialStatus: string;
}) {
  const [status, setStatus] = useState(initialStatus);

  useEffect(() => {
    if (["SUCCEEDED", "FAILED", "EXPIRED"].includes(status)) return;
    const timer = setInterval(async () => {
      try {
        const res = await fetch(`/checkout/api/${paymentId}`);
        if (!res.ok) return;
        const data = (await res.json()) as { status: string };
        setStatus(data.status);
      } catch {
        // keep polling; transient network errors are non-fatal here
      }
    }, 2000);
    return () => clearInterval(timer);
  }, [paymentId, status]);

  const done = ["SUCCEEDED", "FAILED", "EXPIRED"].includes(status);

  return (
    <p
      style={{
        marginTop: 24,
        fontSize: 18,
        fontWeight: 600,
        color:
          status === "SUCCEEDED"
            ? "#1b5e20"
            : status === "EXPIRED" || status === "FAILED"
              ? "#c62828"
              : "#555",
      }}
    >
      {LABELS[status] ?? status}
      {!done && <span style={{ fontSize: 12, color: "#999", marginLeft: 8 }}>polling…</span>}
    </p>
  );
}
