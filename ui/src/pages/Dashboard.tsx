import { useQuery } from "@tanstack/react-query";
import { portalAPI } from "@/lib/api";
import { Link } from "react-router-dom";

export default function Dashboard() {
  const q = useQuery({ queryKey: ["dashboard"], queryFn: portalAPI.dashboard, refetchInterval: 5_000 });

  if (q.isLoading) return <div className="text-muted-foreground">Loading…</div>;
  if (q.error) return <div className="text-destructive">Failed to load dashboard.</div>;
  const d = q.data!;

  return (
    <div className="space-y-6 max-w-5xl">
      <div className="flex items-baseline justify-between">
        <h1 className="text-2xl font-semibold">Dashboard</h1>
        <div className="text-xs text-muted-foreground">
          last 1h · {new Date(d.window_to).toLocaleTimeString()}
        </div>
      </div>

      <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
        <Stat label="Total calls"   value={d.stats.total} />
        <Stat label="Errors"        value={d.stats.errors} accent={d.stats.errors > 0 ? "danger" : undefined} />
        <Stat label="Error rate"    value={`${(d.stats.error_rate * 100).toFixed(1)}%`} />
        <Stat label="Avg duration"  value={`${Math.round(d.stats.avg_duration_ms)}ms`} />
        <Stat label="p50 / p95"     value={`${d.stats.p50_duration_ms} / ${d.stats.p95_duration_ms}ms`} />
        <Stat label="Unique users"  value={d.stats.unique_subjects} />
        <Stat label="Unique tools"  value={d.stats.unique_tools} />
      </div>

      <div>
        <div className="flex items-baseline justify-between mb-2">
          <h2 className="text-lg font-medium">Recent activity</h2>
          <Link to="/audit" className="text-sm text-muted-foreground hover:text-foreground">View all →</Link>
        </div>
        <div className="bg-card text-card-foreground border border-border rounded-lg overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-muted/50 text-muted-foreground">
              <tr>
                <th className="text-left px-3 py-2 font-medium">Time</th>
                <th className="text-left px-3 py-2 font-medium">Tool</th>
                <th className="text-left px-3 py-2 font-medium">User</th>
                <th className="text-left px-3 py-2 font-medium">Result</th>
                <th className="text-right px-3 py-2 font-medium">ms</th>
              </tr>
            </thead>
            <tbody>
              {(d.recent ?? []).map((e) => (
                <tr key={e.id} className="border-t border-border">
                  <td className="px-3 py-1.5 text-muted-foreground mono">{new Date(e.timestamp).toLocaleTimeString()}</td>
                  <td className="px-3 py-1.5">{e.tool_name}</td>
                  <td className="px-3 py-1.5 text-muted-foreground" title={e.user_subject || ""}>
                    {displayUser(e)}
                  </td>
                  <td className="px-3 py-1.5">
                    <span className={e.success ? "text-success" : "text-destructive"}>
                      {e.success ? "ok" : (e.error_category ?? "error")}
                    </span>
                  </td>
                  <td className="px-3 py-1.5 text-right mono">{e.duration_ms}</td>
                </tr>
              ))}
              {d.recent.length === 0 && (
                <tr><td colSpan={5} className="px-3 py-6 text-muted-foreground text-center">No events in the last hour.</td></tr>
              )}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}

function Stat({ label, value, accent }: { label: string; value: number | string; accent?: "danger" }) {
  return (
    <div className="bg-card text-card-foreground border border-border rounded-lg p-3">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className={`text-2xl font-semibold ${accent === "danger" ? "text-destructive" : ""}`}>{value}</div>
    </div>
  );
}

// displayUser prefers email (OIDC), falls back to API-key name, then subject.
// Hovering shows the canonical subject for forensic lookup.
function displayUser(e: { user_email?: string; user_subject?: string }): string {
  if (e.user_email) return e.user_email;
  const sub = e.user_subject ?? "";
  if (sub.startsWith("apikey:")) return sub.slice("apikey:".length);
  return sub || "-";
}
