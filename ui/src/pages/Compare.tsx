import { useQuery } from "@tanstack/react-query";
import { useSearchParams, Link } from "react-router-dom";
import { ArrowLeft } from "lucide-react";
import { portalAPI, type AuditEvent, type AuditPayload, HttpError } from "@/lib/api";

// Compare renders a side-by-side structural diff of two audit events.
// /portal/audit/compare?a=<id>&b=<id>. We fetch both events in parallel
// and walk a JSON-path tree, marking each leaf as one of:
//   - same (value equal in both)
//   - diff (key present in both, values differ)
//   - only-A / only-B (key present in one side)
// Highlights propagate up to parent nodes for quick scanning.
//
// JSON-tree differs from text-diff: it doesn't get confused by key
// reordering between Postgres reads, and it understands leaf-vs-tree
// distinction (a key whose value changes from a string to a map shows
// up as a single diff at that path, not 20 lines of "everything moved").
export default function Compare() {
  const [params] = useSearchParams();
  const aId = params.get("a") ?? "";
  const bId = params.get("b") ?? "";

  const a = useQuery<AuditEvent, HttpError>({
    queryKey: ["audit-event", aId],
    queryFn: () => portalAPI.auditEvent(aId),
    enabled: !!aId,
  });
  const b = useQuery<AuditEvent, HttpError>({
    queryKey: ["audit-event", bId],
    queryFn: () => portalAPI.auditEvent(bId),
    enabled: !!bId,
  });

  if (!aId || !bId) {
    return (
      <div className="space-y-3 max-w-2xl">
        <h1 className="text-2xl font-semibold">Compare events</h1>
        <p className="text-sm text-muted-foreground">
          Open this page with <span className="mono">?a=&lt;id&gt;&amp;b=&lt;id&gt;</span> in the
          URL, or use the Compare button in the audit drawer to stage two events.
        </p>
      </div>
    );
  }

  return (
    <div className="space-y-4 max-w-6xl">
      <div className="flex items-center gap-3">
        <Link to="/audit" className="text-muted-foreground hover:text-foreground">
          <ArrowLeft className="size-4 inline" /> back to Audit
        </Link>
        <h1 className="text-2xl font-semibold">Compare events</h1>
      </div>

      <div className="grid grid-cols-2 gap-4">
        <Header title="A" id={aId} ev={a.data} loading={a.isLoading} error={a.error} />
        <Header title="B" id={bId} ev={b.data} loading={b.isLoading} error={b.error} />
      </div>

      {a.data && b.data && (
        <>
          <Section title="Summary" rows={summaryRows(a.data, b.data)} />
          <PayloadSection title="Request params" aPath={a.data.payload?.request_params} bPath={b.data.payload?.request_params} />
          <PayloadSection title="Response result" aPath={a.data.payload?.response_result} bPath={b.data.payload?.response_result} />
          <PayloadSection title="Response error" aPath={a.data.payload?.response_error} bPath={b.data.payload?.response_error} />
          <NotificationsSection a={a.data.payload} b={b.data.payload} />
        </>
      )}
    </div>
  );
}

function Header({
  title,
  id,
  ev,
  loading,
  error,
}: {
  title: string;
  id: string;
  ev?: AuditEvent;
  loading: boolean;
  error: HttpError | null;
}) {
  return (
    <div className="bg-card text-card-foreground border border-border rounded-lg p-3">
      <div className="text-xs text-muted-foreground mb-1">{title}</div>
      <div className="mono text-sm break-all">{id}</div>
      {loading && <div className="text-xs text-muted-foreground mt-2">Loading...</div>}
      {error && <div className="text-xs text-destructive mt-2">{error.message}</div>}
      {ev && (
        <div className="mt-2 text-sm">
          <span className="font-medium">{ev.tool_name}</span>{" "}
          <span className="text-muted-foreground text-xs mono">{new Date(ev.timestamp).toLocaleString()}</span>{" "}
          <span className={ev.success ? "text-success" : "text-destructive"}>
            {ev.success ? "ok" : (ev.error_category ?? "error")}
          </span>
        </div>
      )}
    </div>
  );
}

type Row = { k: string; a?: React.ReactNode; b?: React.ReactNode; diff: boolean };

function summaryRows(a: AuditEvent, b: AuditEvent): Row[] {
  const rows: Row[] = [];
  const add = (k: string, av?: React.ReactNode, bv?: React.ReactNode) => {
    rows.push({ k, a: av, b: bv, diff: stringify(av) !== stringify(bv) });
  };
  add("Tool", a.tool_name, b.tool_name);
  add("Source", a.source, b.source);
  add("Result", a.success ? "ok" : (a.error_category ?? "error"), b.success ? "ok" : (b.error_category ?? "error"));
  add("Duration", `${a.duration_ms} ms`, `${b.duration_ms} ms`);
  add("User", a.user_email ?? a.user_subject ?? "-", b.user_email ?? b.user_subject ?? "-");
  add("Auth type", a.auth_type ?? "-", b.auth_type ?? "-");
  return rows;
}

function stringify(v: React.ReactNode): string {
  return v === undefined || v === null ? "" : String(v);
}

function Section({ title, rows }: { title: string; rows: Row[] }) {
  const anyDiff = rows.some((r) => r.diff);
  return (
    <div className={`bg-card text-card-foreground border rounded-lg overflow-hidden ${
      anyDiff ? "border-warning/40" : "border-border"
    }`}>
      <div className="px-3 py-2 text-sm font-medium border-b border-border bg-muted/30 flex justify-between">
        <span>{title}</span>
        {anyDiff && <span className="text-xs text-warning">differences</span>}
      </div>
      <table className="w-full text-sm">
        <tbody>
          {rows.map((r) => (
            <tr key={r.k} className={r.diff ? "bg-warning/5" : ""}>
              <td className="px-3 py-1.5 text-muted-foreground border-b border-border w-32">{r.k}</td>
              <td className={`px-3 py-1.5 border-b border-border ${r.diff ? "text-warning" : ""}`}>{r.a ?? <span className="text-muted-foreground italic">-</span>}</td>
              <td className={`px-3 py-1.5 border-b border-border ${r.diff ? "text-warning" : ""}`}>{r.b ?? <span className="text-muted-foreground italic">-</span>}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function PayloadSection({
  title,
  aPath,
  bPath,
}: {
  title: string;
  aPath?: Record<string, unknown>;
  bPath?: Record<string, unknown>;
}) {
  if (!aPath && !bPath) return null;
  const tree = buildDiffTree(aPath, bPath);
  return (
    <div className={`bg-card text-card-foreground border rounded-lg ${
      tree.diffCount > 0 ? "border-warning/40" : "border-border"
    }`}>
      <div className="px-3 py-2 text-sm font-medium border-b border-border bg-muted/30 flex justify-between">
        <span>{title}</span>
        {tree.diffCount > 0 && (
          <span className="text-xs text-warning">{tree.diffCount} difference{tree.diffCount === 1 ? "" : "s"}</span>
        )}
      </div>
      <div className="p-3">
        {tree.children && tree.children.length > 0 ? (
          <DiffTreeView node={tree} />
        ) : tree.diffCount > 0 ? (
          // Leaf-only root with an actual difference: one side is missing
          // the entire payload object, or both sides are non-equal scalars.
          // Render a single-line before/after so the user sees what changed.
          <div className="font-mono text-xs">
            <DiffTreeRow keyName="(root)" node={tree} />
          </div>
        ) : (
          // Both sides are present and equal at root with no leaf children
          // (e.g. both response_error are {}). Don't render a noisy
          // "(root): (undefined)" line.
          <div className="text-xs text-muted-foreground italic">No differences.</div>
        )}
      </div>
    </div>
  );
}

function NotificationsSection({
  a,
  b,
}: {
  a?: AuditPayload;
  b?: AuditPayload;
}) {
  const aN = a?.notifications ?? [];
  const bN = b?.notifications ?? [];
  if (aN.length === 0 && bN.length === 0) return null;
  return (
    <div className={`bg-card text-card-foreground border rounded-lg ${
      aN.length !== bN.length ? "border-warning/40" : "border-border"
    }`}>
      <div className="px-3 py-2 text-sm font-medium border-b border-border bg-muted/30 flex justify-between">
        <span>Notifications</span>
        <span className="text-xs text-muted-foreground">A: {aN.length} / B: {bN.length}</span>
      </div>
      <div className="grid grid-cols-2 gap-3 p-3 text-xs">
        <ol className="space-y-1">
          {aN.map((n, i) => (
            <li key={i} className="mono text-muted-foreground">
              {n.method}{typeof n.params?.message === "string" ? ` "${n.params.message}"` : ""}
            </li>
          ))}
          {aN.length === 0 && <li className="italic text-muted-foreground">none</li>}
        </ol>
        <ol className="space-y-1">
          {bN.map((n, i) => (
            <li key={i} className="mono text-muted-foreground">
              {n.method}{typeof n.params?.message === "string" ? ` "${n.params.message}"` : ""}
            </li>
          ))}
          {bN.length === 0 && <li className="italic text-muted-foreground">none</li>}
        </ol>
      </div>
    </div>
  );
}

// --- Diff tree ---

type DiffNode = {
  kind: "same" | "diff" | "only-a" | "only-b";
  // For object/array nodes, children are the named keys / indices.
  children?: { key: string; node: DiffNode }[];
  aValue?: unknown;
  bValue?: unknown;
  diffCount: number;
};

function buildDiffTree(a: unknown, b: unknown): DiffNode {
  // Treat one-side-undefined as walking the present side fully so the
  // tree shows per-key only-A / only-B markers instead of a single
  // root-level "(undefined) → {…}" leaf.
  if (a === undefined && (isObj(b) || Array.isArray(b))) {
    a = isObj(b) ? {} : [];
  } else if (b === undefined && (isObj(a) || Array.isArray(a))) {
    b = isObj(a) ? {} : [];
  }
  if (isObj(a) && isObj(b)) {
    const keys = new Set<string>([...Object.keys(a), ...Object.keys(b)]);
    const children: { key: string; node: DiffNode }[] = [];
    let diffCount = 0;
    for (const k of Array.from(keys).sort()) {
      const inA = k in a;
      const inB = k in b;
      let node: DiffNode;
      if (inA && inB) {
        node = buildDiffTree((a as Record<string, unknown>)[k], (b as Record<string, unknown>)[k]);
      } else if (inA) {
        node = { kind: "only-a", aValue: (a as Record<string, unknown>)[k], diffCount: 1 };
      } else {
        node = { kind: "only-b", bValue: (b as Record<string, unknown>)[k], diffCount: 1 };
      }
      diffCount += node.diffCount;
      children.push({ key: k, node });
    }
    return { kind: diffCount > 0 ? "diff" : "same", children, diffCount };
  }
  if (Array.isArray(a) && Array.isArray(b)) {
    const len = Math.max(a.length, b.length);
    const children: { key: string; node: DiffNode }[] = [];
    let diffCount = 0;
    for (let i = 0; i < len; i++) {
      const inA = i < a.length;
      const inB = i < b.length;
      let node: DiffNode;
      if (inA && inB) {
        node = buildDiffTree(a[i], b[i]);
      } else if (inA) {
        node = { kind: "only-a", aValue: a[i], diffCount: 1 };
      } else {
        node = { kind: "only-b", bValue: b[i], diffCount: 1 };
      }
      diffCount += node.diffCount;
      children.push({ key: String(i), node });
    }
    return { kind: diffCount > 0 ? "diff" : "same", children, diffCount };
  }
  // Leaves: deep-equal via JSON.stringify (cheap; both values are
  // already in JSON-shape from the audit_payloads JSONB columns).
  if (JSON.stringify(a) === JSON.stringify(b)) {
    return { kind: "same", aValue: a, bValue: b, diffCount: 0 };
  }
  return { kind: "diff", aValue: a, bValue: b, diffCount: 1 };
}

function isObj(v: unknown): v is Record<string, unknown> {
  return v !== null && typeof v === "object" && !Array.isArray(v);
}

function DiffTreeView({ node, nested = false }: { node: DiffNode; nested?: boolean }) {
  if (!node.children) return null;
  // Indentation comes from each nested <ul>'s own left padding; the
  // outer (root) <ul> renders flush so deep trees don't walk off-panel.
  return (
    <ul className={nested ? "font-mono text-xs pl-3" : "font-mono text-xs"}>
      {node.children.map((c) => (
        <li key={c.key}>
          <DiffTreeRow keyName={c.key} node={c.node} />
          {c.node.children && c.node.children.length > 0 && (
            <DiffTreeView node={c.node} nested />
          )}
        </li>
      ))}
    </ul>
  );
}

function DiffTreeRow({ keyName, node }: { keyName: string; node: DiffNode }) {
  if (node.children && node.children.length > 0) {
    return (
      <span className={node.kind === "diff" ? "text-warning" : "text-muted-foreground"}>
        {keyName}:
      </span>
    );
  }
  switch (node.kind) {
    case "same":
      return (
        <span>
          <span className="text-muted-foreground">{keyName}:</span> {fmt(node.aValue)}
        </span>
      );
    case "diff":
      return (
        <span>
          <span className="text-warning">{keyName}:</span>{" "}
          <span className="text-destructive">{fmt(node.aValue)}</span>
          {" → "}
          <span className="text-success">{fmt(node.bValue)}</span>
        </span>
      );
    case "only-a":
      return (
        <span className="text-destructive">
          - {keyName}: {fmt(node.aValue)}
        </span>
      );
    case "only-b":
      return (
        <span className="text-success">
          + {keyName}: {fmt(node.bValue)}
        </span>
      );
  }
}

function fmt(v: unknown): string {
  if (v === null) return "null";
  if (v === undefined) return "(undefined)";
  if (typeof v === "string") return JSON.stringify(v);
  if (typeof v === "number" || typeof v === "boolean") return String(v);
  // Compact rendering for nested structures so the row stays single-line.
  const s = JSON.stringify(v);
  return s.length > 80 ? s.slice(0, 77) + "..." : s;
}
