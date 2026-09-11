// Thin client for the Go read API (cmd/api). Base URL is configurable so the
// dev server (port 5173) can hit the API (port 8081), and prod can hit the
// same origin behind an ingress.
const BASE =
  (import.meta.env.VITE_API_URL as string | undefined) ?? "http://localhost:8081";

export interface Stats {
  total_prs: number;
  avg_risk_score: number;
  total_reviews: number;
}

export interface PRSummary {
  repo: string;
  number: number;
  title: string;
  author: string;
  lines_added: number;
  lines_removed: number;
  files_changed: number;
  risk_score: number;
  status: string;
  opened_at: string;
}

export interface Hotspot {
  file_path: string;
  bugfix_count: number;
  last_seen: string;
}

async function getJSON<T>(path: string): Promise<T> {
  const res = await fetch(`${BASE}${path}`);
  if (!res.ok) throw new Error(`${path} -> ${res.status}`);
  return (await res.json()) as T;
}

export const api = {
  stats: () => getJSON<Stats>("/api/stats"),
  prs: () => getJSON<PRSummary[]>("/api/prs?limit=50"),
  hotspots: () => getJSON<Hotspot[]>("/api/hotspots?limit=10"),
};
