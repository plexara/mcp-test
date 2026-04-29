import { useQuery } from "@tanstack/react-query";
import { portalAPI } from "@/lib/api";

export default function Config() {
  const q = useQuery({ queryKey: ["server"], queryFn: portalAPI.server });
  if (q.isLoading) return <div className="text-muted-foreground">Loading…</div>;
  if (q.error) return <div className="text-destructive">{(q.error as Error).message}</div>;

  return (
    <div className="space-y-4 max-w-4xl">
      <div>
        <h1 className="text-2xl font-semibold">Configuration</h1>
        <p className="text-muted-foreground text-sm">Read-only view of the running config (secrets redacted).</p>
      </div>
      <div className="text-sm text-muted-foreground">
        <span className="mono">{q.data!.version}</span> · <span className="mono">{q.data!.commit}</span> · <span className="mono">{q.data!.date}</span>
      </div>
      <pre className="bg-card text-card-foreground border border-border rounded-lg p-3 mono text-xs overflow-auto">
{JSON.stringify(q.data!.config, null, 2)}
      </pre>
    </div>
  );
}
