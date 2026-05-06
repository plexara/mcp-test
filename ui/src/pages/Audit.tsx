import { useQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { Radio, GitCompare, Filter, X } from "lucide-react";
import { portalAPI, streamAuditEvents, type AuditEvent } from "@/lib/api";
import { EventDrawer } from "@/components/EventDrawer";

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

// JSONB filter shape. Matches the parseQueryFilter syntax in
// pkg/httpsrv/portal_api.go (param.<dotted>=v / response.<dotted>=v /
// header.<name>=v / has=<col>).
type JsonFilter = {
  source: "param" | "response" | "header" | "has";
  // For param/response/header: the dotted path (header is single-segment).
  // For has: the column name.
  path: string;
  // For param/response/header only.
  value: string;
};

const HAS_KEYS = [
  "request_params",
  "request_headers",
  "response_result",
  "response_error",
  "notifications",
  "replayed_from",
];

const COMPARE_KEY = "audit-compare-stash";

export default function Audit() {
  const [params, setParams] = useSearchParams();
  const navigate = useNavigate();

  const [tool, setTool] = useState("");
  const [user, setUser] = useState("");
  const [success, setSuccess] = useState<"" | "true" | "false">("");
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(0);
  const [showFilters, setShowFilters] = useState(false);
  const [jsonFilters, setJsonFilters] = useState<JsonFilter[]>([]);
  const [liveTail, setLiveTail] = useState(false);
  const [tailEvents, setTailEvents] = useState<AuditEvent[]>([]);
  const [tailError, setTailError] = useState<string | null>(null);
  const limit = 50;

  const debouncedSearch = useDebounced(search, 300);
  const debouncedTool = useDebounced(tool, 300);
  const debouncedUser = useDebounced(user, 300);

  // Drawer selection comes from URL ?id= so deep-linking works.
  const selectedId = params.get("id");

  // Compare-to: the most recently stashed event id from the drawer's
  // Compare button. localStorage persists across reloads.
  const [compareId, setCompareId] = useState<string | null>(() => localStorage.getItem(COMPARE_KEY));

  const qs = useMemo(() => {
    const u = new URLSearchParams();
    if (debouncedTool) u.set("tool", debouncedTool);
    if (debouncedUser) u.set("user", debouncedUser);
    if (success) u.set("success", success);
    if (debouncedSearch) u.set("q", debouncedSearch);
    for (const f of jsonFilters) {
      if (f.source === "has") {
        if (f.path) u.append("has", f.path);
      } else if (f.path && f.value) {
        u.append(`${f.source}.${f.path}`, f.value);
      }
    }
    u.set("limit", String(limit));
    u.set("offset", String(page * limit));
    return u;
  }, [debouncedTool, debouncedUser, success, debouncedSearch, jsonFilters, page]);

  const q = useQuery({
    queryKey: ["audit", qs.toString()],
    queryFn: () => portalAPI.audit(qs.toString()),
    placeholderData: (p) => p,
    refetchInterval: liveTail ? false : undefined,
  });

  // Live tail: open the SSE stream when the toggle is on; close on toggle
  // off or unmount. Incoming events go into the cap-20 buffer shown above
  // the table; the table itself stays a historical-filter view to avoid a
  // refetch-per-event storm under load (use the buffer for the live read,
  // page the table for context).
  useEffect(() => {
    if (!liveTail) {
      setTailEvents([]);
      setTailError(null);
      return;
    }
    const stop = streamAuditEvents(
      (ev) => {
        setTailEvents((prev) => [ev, ...prev].slice(0, 20));
      },
      (err) => setTailError(err.message),
    );
    return stop;
  }, [liveTail]);

  const totalPages = q.data ? Math.ceil(q.data.total / limit) : 1;

  function selectEvent(id: string | null) {
    const next = new URLSearchParams(params);
    if (id) next.set("id", id);
    else next.delete("id");
    setParams(next, { replace: true });
  }

  function stashCompare(id: string) {
    localStorage.setItem(COMPARE_KEY, id);
    setCompareId(id);
  }

  function openCompare() {
    if (!compareId || !selectedId || compareId === selectedId) return;
    navigate(`/audit/compare?a=${encodeURIComponent(compareId)}&b=${encodeURIComponent(selectedId)}`);
  }

  return (
    <div className="space-y-4 max-w-6xl">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Audit</h1>
        <div className="flex items-center gap-2">
          <button
            onClick={() => setShowFilters((v) => !v)}
            className={`flex items-center gap-1 text-sm px-2 py-1 rounded border transition-colors ${
              showFilters || jsonFilters.length > 0
                ? "border-primary text-primary"
                : "border-border text-muted-foreground hover:text-foreground"
            }`}
          >
            <Filter className="size-3.5" />
            JSONB filters{jsonFilters.length > 0 ? ` (${jsonFilters.length})` : ""}
          </button>
          <button
            onClick={() => setLiveTail((v) => !v)}
            className={`flex items-center gap-1 text-sm px-2 py-1 rounded border transition-colors ${
              liveTail
                ? "border-success text-success bg-success/10"
                : "border-border text-muted-foreground hover:text-foreground"
            }`}
            title={liveTail ? "Disconnect SSE stream" : "Subscribe to SSE stream"}
          >
            <Radio className={`size-3.5 ${liveTail ? "animate-pulse" : ""}`} />
            Live tail
          </button>
        </div>
      </div>

      <div className="grid grid-cols-2 md:grid-cols-4 gap-2 bg-card text-card-foreground border border-border rounded-lg p-3">
        <Field label="Tool"><input className={inputCls + " w-full"} value={tool} onChange={(e) => { setTool(e.target.value); setPage(0); }} /></Field>
        <Field label="User"><input className={inputCls + " w-full"} value={user} onChange={(e) => { setUser(e.target.value); setPage(0); }} /></Field>
        <Field label="Status">
          <select className={inputCls + " w-full"} value={success} onChange={(e) => { setSuccess(e.target.value as "" | "true" | "false"); setPage(0); }}>
            <option value="">all</option>
            <option value="true">success</option>
            <option value="false">error</option>
          </select>
        </Field>
        <Field label="Search"><input className={inputCls + " w-full"} value={search} onChange={(e) => { setSearch(e.target.value); setPage(0); }} /></Field>
      </div>

      {showFilters && (
        <JsonFiltersEditor
          filters={jsonFilters}
          onChange={(fs) => { setJsonFilters(fs); setPage(0); }}
        />
      )}

      {liveTail && (
        <div className="bg-card text-card-foreground border border-border rounded-lg p-3 text-xs">
          <div className="flex items-center justify-between mb-2">
            <span className="text-muted-foreground">Live tail (most recent first)</span>
            {tailError && <span className="text-destructive">{tailError}</span>}
          </div>
          {tailEvents.length === 0 ? (
            <div className="text-muted-foreground italic">Waiting for events...</div>
          ) : (
            <ul className="space-y-1 mono">
              {tailEvents.map((e) => (
                <li key={e.id} className="flex justify-between gap-3">
                  <button
                    className="truncate text-left hover:underline"
                    onClick={() => selectEvent(e.id)}
                  >
                    {new Date(e.timestamp).toLocaleTimeString()} {e.tool_name}
                  </button>
                  <span className={e.success ? "text-success" : "text-destructive"}>
                    {e.success ? "ok" : (e.error_category ?? "error")}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}

      {compareId && (
        <div className="bg-card text-card-foreground border border-border rounded-lg p-2 text-xs flex items-center justify-between">
          <div className="text-muted-foreground">
            Stashed for compare:{" "}
            <span className="mono">{compareId.slice(0, 8)}...{compareId.slice(-4)}</span>
          </div>
          <div className="flex items-center gap-2">
            {selectedId && selectedId !== compareId && (
              <button
                onClick={openCompare}
                className="flex items-center gap-1 px-2 py-1 rounded border border-border hover:bg-muted"
              >
                <GitCompare className="size-3.5" /> Compare with selected
              </button>
            )}
            <button
              onClick={() => { localStorage.removeItem(COMPARE_KEY); setCompareId(null); }}
              className="p-1 text-muted-foreground hover:text-foreground"
              aria-label="Clear stashed compare event"
            >
              <X className="size-3.5" />
            </button>
          </div>
        </div>
      )}

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
              <tr
                key={e.id}
                className={`border-t border-border hover:bg-muted/30 transition-colors cursor-pointer ${
                  e.id === selectedId ? "bg-muted/40" : ""
                }`}
                onClick={() => selectEvent(e.id)}
              >
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

      <EventDrawer
        eventId={selectedId}
        onClose={() => selectEvent(null)}
        onCompareSelect={stashCompare}
      />
    </div>
  );
}

function JsonFiltersEditor({
  filters,
  onChange,
}: {
  filters: JsonFilter[];
  onChange: (fs: JsonFilter[]) => void;
}) {
  const newRef = useRef<HTMLInputElement>(null);
  const [draft, setDraft] = useState<JsonFilter>({ source: "param", path: "", value: "" });

  function add() {
    if (draft.source === "has") {
      if (!draft.path) return;
      onChange([...filters, { ...draft, value: "" }]);
    } else {
      if (!draft.path || !draft.value) return;
      onChange([...filters, { ...draft }]);
    }
    setDraft({ source: draft.source, path: "", value: "" });
    newRef.current?.focus();
  }

  function remove(i: number) {
    onChange(filters.filter((_, j) => j !== i));
  }

  return (
    <div className="bg-card text-card-foreground border border-border rounded-lg p-3 space-y-2">
      <div className="text-xs text-muted-foreground">
        JSONB path filters narrow against <span className="mono">audit_payloads</span>.
        Values are type-detected (true/false → bool, integers / floats → number,
        else string; quote to force string). Header values are always strings.
      </div>
      <ul className="space-y-1">
        {filters.map((f, i) => (
          <li key={i} className="flex items-center gap-2 text-sm">
            <span className="mono text-muted-foreground">
              {f.source === "has" ? `has=${f.path}` : `${f.source}.${f.path}=${f.value}`}
            </span>
            <button
              onClick={() => remove(i)}
              className="text-muted-foreground hover:text-foreground"
              aria-label={`remove filter ${i}`}
            >
              <X className="size-3.5" />
            </button>
          </li>
        ))}
      </ul>
      <div className="flex flex-wrap items-center gap-2">
        <select
          className={inputCls + " w-auto"}
          value={draft.source}
          onChange={(e) => setDraft({ source: e.target.value as JsonFilter["source"], path: "", value: "" })}
        >
          <option value="param">param.</option>
          <option value="response">response.</option>
          <option value="header">header.</option>
          <option value="has">has=</option>
        </select>
        {draft.source === "has" ? (
          <select
            className={inputCls + " flex-1"}
            value={draft.path}
            onChange={(e) => setDraft({ ...draft, path: e.target.value })}
          >
            <option value="">column...</option>
            {HAS_KEYS.map((k) => <option key={k} value={k}>{k}</option>)}
          </select>
        ) : (
          <>
            <input
              ref={newRef}
              className={inputCls + " flex-1"}
              placeholder={draft.source === "header" ? "header name" : "dotted.path"}
              value={draft.path}
              onChange={(e) => setDraft({ ...draft, path: e.target.value })}
              onKeyDown={(e) => { if (e.key === "Enter") add(); }}
            />
            <input
              className={inputCls + " flex-1"}
              placeholder="value"
              value={draft.value}
              onChange={(e) => setDraft({ ...draft, value: e.target.value })}
              onKeyDown={(e) => { if (e.key === "Enter") add(); }}
            />
          </>
        )}
        <button
          onClick={add}
          className="px-2 py-1 rounded border border-border text-sm hover:bg-muted"
        >
          add
        </button>
      </div>
    </div>
  );
}

const inputCls = "bg-background border border-input rounded px-2 py-1 text-sm focus:outline-none focus:ring-2 focus:ring-ring";

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="block">
      <div className="text-xs text-muted-foreground mb-1">{label}</div>
      {children}
    </label>
  );
}

function displayUser(e: { user_email?: string; user_subject?: string }): string {
  if (e.user_email) return e.user_email;
  const sub = e.user_subject ?? "";
  if (sub.startsWith("apikey:")) return sub.slice("apikey:".length);
  return sub || "-";
}
