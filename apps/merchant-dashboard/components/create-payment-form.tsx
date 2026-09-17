"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { createPayment } from "@/lib/api";

export default function CreatePaymentForm() {
  const router = useRouter();
  const [amount, setAmount] = useState("");
  const [reference, setReference] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    const rupees = Number(amount);
    if (!Number.isFinite(rupees) || rupees <= 0) {
      setError("Enter a valid amount in rupees");
      return;
    }
    if (!reference.trim()) {
      setError("Enter an order reference");
      return;
    }
    setBusy(true);
    setError(null);
    const res = await createPayment(
      Math.round(rupees * 100),
      reference.trim(),
      crypto.randomUUID(),
    );
    setBusy(false);
    if (!res.ok) {
      setError(res.error ?? "Payment creation failed");
      return;
    }
    setAmount("");
    setReference("");
    router.refresh();
  }

  return (
    <form onSubmit={submit} className="create-form">
      <input
        value={amount}
        onChange={(e) => setAmount(e.target.value)}
        placeholder="Amount ₹"
        inputMode="decimal"
        disabled={busy}
      />
      <input
        value={reference}
        onChange={(e) => setReference(e.target.value)}
        placeholder="Order reference"
        disabled={busy}
      />
      <button type="submit" disabled={busy}>
        {busy ? "Creating…" : "Create payment"}
      </button>
      {error && <span className="form-error">{error}</span>}
      <style jsx>{`
        .create-form {
          display: flex;
          gap: 8px;
          align-items: center;
          margin: 16px 0;
          flex-wrap: wrap;
        }
        input {
          padding: 8px;
          border: 1px solid #ccc;
          border-radius: 6px;
        }
        button {
          padding: 8px 14px;
          border-radius: 6px;
          border: none;
          background: #1a1a1a;
          color: white;
          cursor: pointer;
        }
        .form-error {
          color: #c62828;
        }
      `}</style>
    </form>
  );
}
