import { useEffect, useState } from "react";
import { api, type Hotspot, type PRSummary, type Stats } from "./api";

function riskColor(score: number): string {
  if (score >= 70) return "#e5484d";
  if (score >= 40) return "#f5a623";
  return "#30a46c";
}

function StatCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="card stat">
      <div className="stat-value">{value}</div>
      <div className="stat-label">{label}</div>
    </div>
  );
}

// Simple CSS bar chart of the most recent PRs by risk score (no chart library
// needed — keeps the build light).
function RiskChart({ prs }: { prs: PRSummary[] }) {
  const rows = prs.slice(0, 12);
  return (
    <div className="card">
      <h2>Risk score by PR</h2>
      <div className="chart">
        {rows.length === 0 && <p className="muted">No PRs yet.</p>}
        {rows.map((p) => (
          <div className="bar-row" key={`${p.repo}#${p.number}`}>
            <span className="bar-label">#{p.number}</span>
            <div className="bar-track">
              <div
                className="bar-fill"
                style={{ width: `${p.risk_score}%`, background: riskColor(p.risk_score) }}
              />
            </div>
            <span className="bar-value">{p.risk_score}</span>
          </div>
        ))}
      </div>
    </div>
  );
}

function PRTable({ prs }: { prs: PRSummary[] }) {
  return (
    <div className="card">
      <h2>Recent pull requests</h2>
      <table>
        <thead>
          <tr>
            <th>PR</th>
            <th>Title</th>
            <th>Author</th>
            <th>+/-</th>
            <th>Files</th>
            <th>Risk</th>
          </tr>
        </thead>
        <tbody>
          {prs.map((p) => (
            <tr key={`${p.repo}#${p.number}`}>
              <td className="mono">{p.repo}#{p.number}</td>
              <td>{p.title}</td>
              <td>{p.author}</td>
              <td className="mono">
                <span className="add">+{p.lines_added}</span>{" "}
                <span className="del">-{p.lines_removed}</span>
              </td>
              <td className="mono">{p.files_changed}</td>
              <td>
                <span className="pill" style={{ background: riskColor(p.risk_score) }}>
                  {p.risk_score}
                </span>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function Hotspots({ rows }: { rows: Hotspot[] }) {
  return (
    <div className="card">
      <h2>Hotspot files</h2>
      <p className="muted">Files that appear most often in bug-fix PRs.</p>
      <table>
        <thead>
          <tr>
            <th>File</th>
            <th>Bug-fix count</th>
          </tr>
        </thead>
        <tbody>
          {rows.length === 0 && (
            <tr>
              <td colSpan={2} className="muted">No hotspots yet.</td>
            </tr>
          )}
          {rows.map((h) => (
            <tr key={h.file_path}>
              <td className="mono">{h.file_path}</td>
              <td className="mono">{h.bugfix_count}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export default function App() {
  const [stats, setStats] = useState<Stats | null>(null);
  const [prs, setPRs] = useState<PRSummary[]>([]);
  const [hotspots, setHotspots] = useState<Hotspot[]>([]);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    Promise.all([api.stats(), api.prs(), api.hotspots()])
      .then(([s, p, h]) => {
        setStats(s);
        setPRs(p ?? []);
        setHotspots(h ?? []);
      })
      .catch((e) => setError(String(e)));
  }, []);

  return (
    <div className="app">
      <header>
        <h1>PR Insights</h1>
        <p className="muted">AI Code Review &amp; PR Insights Platform</p>
      </header>

      {error && (
        <div className="card error">
          Could not reach the API ({error}). Start it with the read API on
          <code> :8081</code> and run the demo to populate data.
        </div>
      )}

      <section className="stats-row">
        <StatCard label="Pull requests" value={String(stats?.total_prs ?? 0)} />
        <StatCard label="Avg risk score" value={(stats?.avg_risk_score ?? 0).toFixed(0)} />
        <StatCard label="Reviews run" value={String(stats?.total_reviews ?? 0)} />
      </section>

      <RiskChart prs={prs} />
      <PRTable prs={prs} />
      <Hotspots rows={hotspots} />
    </div>
  );
}
