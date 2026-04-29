import { useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { portalAPI } from "@/lib/api";

// useDebounced returns `value` after `ms` of stillness; used to avoid
// firing an audit query on every keystroke.
function useDebounced<T>(value: T, ms = 300): T {
  const [v, setV] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setV(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return v;
}

export default function Audit() {
  const [tool, setTool] = useState("");
  const [user, setUser] = useState("");
  const [success, setSuccess] = useState<"" | "true" | "false">("");
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(0);
  const limit = 50;

  const debouncedSearch = useDebounced(search, 300);
  const debouncedTool = useDebounced(tool, 300);
  const debouncedUser = useDebounced(user, 300);

  const qs = new URLSearchParams();
  if (debouncedTool)   qs.set("tool", debouncedTool);
  if (debouncedUser)   qs.set("user", debouncedUser);
  if (success)         qs.set("success", success);
  if (debouncedSearch) qs.set("q", debouncedSearch);
  qs.set("limit", String(limit));
  qs.set("offset", String(page * limit));

  const q = useQuery({
    queryKey: ["audit", qs.toString()],
    queryFn: () => portalAPI.audit(qs.toString()),
    placeholderData: (p) => p,
  });

  const totalPages = q.data ? Math.ceil(q.data.total / limit) : 1;

  return (
    <div className="space-y-4 max-w-6xl">
      <h1 className="text-2xl font-semibold">Audit</h1>
      <div className="grid grid-cols-2 md:grid-cols-4 gap-2 bg-card text-card-foreground border border-border rounded-lg p-3">
        <Field label="Tool"><input className={inputCls} value={tool} onChange={(e) => { setTool(e.target.value); setPage(0); }} /></Field>
        <Field label="User"><input className={inputCls} value={user} onChange={(e) => { setUser(e.target.value); setPage(0); }} /></Field>
        <Field label="Status">
          <select className={inputCls} value={success} onChange={(e) => { setSuccess(e.target.value as "" | "true" | "false"); setPage(0); }}>
            <option value="">all</option>
            <option value="true">success</option>
            <option value="false">error</option>
          </select>
        </Field>
        <Field label="Search"><input className={inputCls} value={search} onChange={(e) => { setSearch(e.target.value); setPage(0); }} /></Field>
      </div>

      <div className="bg-card text-card-foreground border border-border rounded-lg overflow-hidden">
        <table className="w-full text-sm">
          <thead className="bg-muted/50 text-muted-foreground">
            <tr>
              <th className="text-left px-3 py-2 font-medium">Time</th>
              <th className="text-left px-3 py-2 font-medium">Tool</th>
              <th className="text-left px-3 py-2 font-medium">User</th>
              <th className="text-left px-3 py-2 font-medium">Source</th>
              <th className="text-left px-3 py-2 font-medium">Result</th>
              <th className="text-right px-3 py-2 font-medium">ms</th>
            </tr>
          </thead>
          <tbody>
            {q.data?.events.map((e) => (
              <tr key={e.id} className="border-t border-border hover:bg-muted/30 transition-colors">
                <td className="px-3 py-1.5 text-muted-foreground mono">{new Date(e.timestamp).toLocaleString()}</td>
                <td className="px-3 py-1.5">{e.tool_name}</td>
                <td className="px-3 py-1.5 text-muted-foreground" title={e.user_subject || ""}>
                  {displayUser(e)}
                </td>
                <td className="px-3 py-1.5 text-muted-foreground">{e.source}</td>
                <td className="px-3 py-1.5">
                  <span className={e.success ? "text-success" : "text-destructive"}>
                    {e.success ? "ok" : (e.error_category ?? "error")}
                  </span>
                </td>
                <td className="px-3 py-1.5 text-right mono">{e.duration_ms}</td>
              </tr>
            ))}
            {q.data && q.data.events.length === 0 && (
              <tr><td colSpan={6} className="px-3 py-6 text-center text-muted-foreground">No matching events.</td></tr>
            )}
          </tbody>
        </table>
      </div>

      {q.data && totalPages > 1 && (
        <div className="flex justify-between text-sm">
          <button disabled={page === 0} onClick={() => setPage((p) => Math.max(0, p - 1))} className="text-muted-foreground hover:text-foreground disabled:opacity-30">‹ prev</button>
          <span className="text-muted-foreground">page {page + 1} / {totalPages}</span>
          <button disabled={page + 1 >= totalPages} onClick={() => setPage((p) => p + 1)} className="text-muted-foreground hover:text-foreground disabled:opacity-30">next ›</button>
        </div>
      )}
    </div>
  );
}

const inputCls = "w-full bg-background border border-input rounded px-2 py-1 text-sm focus:outline-none focus:ring-2 focus:ring-ring";

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="block">
      <div className="text-xs text-muted-foreground mb-1">{label}</div>
      {children}
    </label>
  );
}

// displayUser shows the most human-readable identifier we have for the audit
// row's caller: email first (OIDC), then API-key name ("apikey:NAME"), then
// the raw subject (Keycloak UUID, etc.) as a last resort.
function displayUser(e: { user_email?: string; user_subject?: string }): string {
  if (e.user_email) return e.user_email;
  const sub = e.user_subject ?? "";
  if (sub.startsWith("apikey:")) return sub.slice("apikey:".length);
  return sub || "-";
}
