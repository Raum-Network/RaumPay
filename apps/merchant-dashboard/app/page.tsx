import { listPayments, type Payment } from "@/lib/api";
import CreatePaymentForm from "@/components/create-payment-form";
import styles from "./page.module.css";

export const dynamic = "force-dynamic";

function fmt(paise: number): string {
  return `₹${(paise / 100).toFixed(2)}`;
}

export default async function Home() {
  let payments: Payment[] = [];
  let error: string | null = null;
  try {
    payments = await listPayments();
  } catch {
    error = "RaumPay API unreachable — start it with RAUMPAY_MODE=sandbox";
  }

  return (
    <div className={styles.page}>
      <main className={styles.main}>
        <h1>RaumPay Merchant Dashboard</h1>
        <p className={styles.subtitle}>
          Local sandbox · Digital Rupee (e₹) · provider: mock_cbdc
        </p>
        {error && <p className={styles.error}>{error}</p>}
        <CreatePaymentForm />
        <table className={styles.table}>
          <thead>
            <tr>
              <th>Payment</th>
              <th>Order</th>
              <th>Amount</th>
              <th>Status</th>
            </tr>
          </thead>
          <tbody>
            {payments.length === 0 && (
              <tr>
                <td colSpan={4}>No payments yet.</td>
              </tr>
            )}
            {payments.map((p) => (
              <tr key={p.id}>
                <td>
                  <a href={`/payments/${p.id}`}>{p.id}</a>
                </td>
                <td>{p.merchant_reference}</td>
                <td>{fmt(p.amount)}</td>
                <td>{p.status}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </main>
    </div>
  );
}
