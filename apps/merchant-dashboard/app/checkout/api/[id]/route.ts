// ponytail: thin proxy to the Go API's public checkout endpoint so the browser
// never sees API topology; add rate limiting if this ever leaves the sandbox.
const API = process.env.RAUMPAY_API_URL ?? "http://127.0.0.1:8080";

export async function GET(
  _request: Request,
  { params }: { params: Promise<{ id: string }> },
) {
  const { id } = await params;
  const res = await fetch(`${API}/v1/checkout/${id}`, { cache: "no-store" });
  const body = await res.text();
  return new Response(body, {
    status: res.status,
    headers: { "Content-Type": "application/json", "Cache-Control": "no-store" },
  });
}
