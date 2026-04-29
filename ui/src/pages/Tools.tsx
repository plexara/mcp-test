import { useQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { portalAPI, type ToolMeta } from "@/lib/api";
import ToolForm from "@/components/ToolForm";

export default function Tools() {
  const { name } = useParams();
  const navigate = useNavigate();
  const list = useQuery({ queryKey: ["tools"], queryFn: portalAPI.tools });

  const tools = list.data?.tools ?? [];
  const grouped = useMemo(() => groupBy(tools, (t) => t.group || "other"), [tools]);
  const selected = name ? tools.find((t) => t.name === name) : tools[0];

  useEffect(() => {
    if (!name && tools.length > 0) navigate(`/tools/${tools[0].name}`, { replace: true });
  }, [name, tools, navigate]);

  return (
    <div className="grid grid-cols-[16rem_1fr] gap-6 max-w-6xl">
      <aside className="space-y-4">
        <h1 className="text-xl font-semibold">Tools</h1>
        {Object.entries(grouped).map(([group, items]) => (
          <div key={group}>
            <div className="text-xs uppercase tracking-wide text-muted-foreground mb-1">{group}</div>
            <div className="flex flex-col gap-0.5">
              {items.map((t) => (
                <button
                  key={t.name}
                  onClick={() => navigate(`/tools/${t.name}`)}
                  className={`text-left px-2 py-1.5 rounded text-sm transition-colors ${
                    t.name === selected?.name
                      ? "bg-primary text-primary-foreground"
                      : "hover:bg-muted text-foreground/90"
                  }`}
                >
                  {t.name}
                </button>
              ))}
            </div>
          </div>
        ))}
      </aside>
      <main>
        {selected ? <ToolDetail tool={selected} /> : <div className="text-muted-foreground">No tools registered.</div>}
      </main>
    </div>
  );
}

function ToolDetail({ tool }: { tool: ToolMeta }) {
  const [tab, setTab] = useState<"overview" | "tryit">("overview");
  return (
    <div className="space-y-4">
      <div>
        <div className="text-xs uppercase tracking-wide text-muted-foreground">{tool.group}</div>
        <h2 className="text-2xl font-semibold">{tool.name}</h2>
        <p className="text-muted-foreground mt-1">{tool.description}</p>
      </div>
      <div className="border-b border-border">
        <Tab active={tab === "overview"} onClick={() => setTab("overview")}>Overview</Tab>
        <Tab active={tab === "tryit"} onClick={() => setTab("tryit")}>Try It</Tab>
      </div>
      {tab === "overview" && (
        <pre className="bg-card text-card-foreground border border-border rounded-lg p-3 mono text-xs overflow-auto">
{JSON.stringify(tool, null, 2)}
        </pre>
      )}
      {tab === "tryit" && <ToolForm toolName={tool.name} inputSchema={tool.input_schema} />}
    </div>
  );
}

function Tab({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      onClick={onClick}
      className={`px-3 py-1.5 text-sm border-b-2 -mb-px transition-colors ${
        active
          ? "border-primary text-foreground"
          : "border-transparent text-muted-foreground hover:text-foreground"
      }`}
    >
      {children}
    </button>
  );
}

function groupBy<T, K extends string>(arr: T[], key: (x: T) => K): Record<K, T[]> {
  return arr.reduce((acc, x) => {
    const k = key(x);
    (acc[k] ??= []).push(x);
    return acc;
  }, {} as Record<K, T[]>);
}
