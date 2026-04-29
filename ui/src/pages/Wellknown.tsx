import { useQuery } from "@tanstack/react-query";
import { portalAPI } from "@/lib/api";

export default function Wellknown() {
  const q = useQuery({ queryKey: ["wellknown"], queryFn: portalAPI.wellknown });
  if (q.isLoading) return <div className="text-muted-foreground">Loading…</div>;
  if (q.error) return <div className="text-destructive">{(q.error as Error).message}</div>;
  const d = q.data!;

  return (
    <div className="space-y-4 max-w-3xl">
      <div>
        <h1 className="text-2xl font-semibold">Discovery</h1>
        <p className="text-muted-foreground text-sm">
          What an MCP gateway sees when it discovers this server.
        </p>
      </div>

      <div className="bg-card text-card-foreground border border-border rounded-lg p-3 space-y-2 text-sm">
        <Row k="MCP endpoint"               v={<code className="mono">{d.mcp_endpoint}</code>} />
        <Row k="Protected resource metadata" v={<a className="underline text-primary" href={d.protected_resource_url} target="_blank" rel="noreferrer">{d.protected_resource_url}</a>} />
        <Row k="OIDC enabled"               v={d.oidc_enabled ? "yes" : "no"} />
        <Row k="Authorization server"       v={<code className="mono">{d.authorization_server || "(unset)"}</code>} />
        <Row k="Audience"                   v={<code className="mono">{d.audience}</code>} />
      </div>
    </div>
  );
}

function Row({ k, v }: { k: string; v: React.ReactNode }) {
  return (
    <div className="grid grid-cols-[12rem_1fr] gap-3 items-baseline">
      <div className="text-muted-foreground">{k}</div>
      <div>{v}</div>
    </div>
  );
}
