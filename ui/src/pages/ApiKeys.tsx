import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { adminAPI } from "@/lib/api";

export default function ApiKeys() {
  const qc = useQueryClient();
  const list = useQuery({ queryKey: ["keys"], queryFn: () => adminAPI.listKeys() });
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [created, setCreated] = useState<{ name: string; plaintext: string } | null>(null);
  const [copyState, setCopyState] = useState<"idle" | "copied" | "failed">("idle");

  const create = useMutation({
    mutationFn: () => adminAPI.createKey(name, description),
    onSuccess: (res) => {
      setCreated({ name: res.key.name, plaintext: res.plaintext });
      setCopyState("idle");
      setName(""); setDescription("");
      qc.invalidateQueries({ queryKey: ["keys"] });
    },
  });
  const remove = useMutation({
    mutationFn: (n: string) => adminAPI.deleteKey(n),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["keys"] }),
  });

  // Block accidental tab-close while a freshly minted key is on screen; the
  // user won't see the plaintext again. Only active when `created` is set.
  useEffect(() => {
    if (!created) return;
    const handler = (e: BeforeUnloadEvent) => {
      e.preventDefault();
      e.returnValue = "";
    };
    window.addEventListener("beforeunload", handler);
    return () => window.removeEventListener("beforeunload", handler);
  }, [created]);

  async function copyKey() {
    if (!created) return;
    try {
      await navigator.clipboard.writeText(created.plaintext);
      setCopyState("copied");
      setTimeout(() => setCopyState("idle"), 1500);
    } catch {
      setCopyState("failed");
    }
  }

  return (
    <div className="space-y-6 max-w-3xl">
      <h1 className="text-2xl font-semibold">API keys</h1>

      <form
        onSubmit={(e) => { e.preventDefault(); create.mutate(); }}
        className="grid grid-cols-1 md:grid-cols-[1fr_2fr_auto] gap-2 bg-card text-card-foreground border border-border rounded-lg p-3"
      >
        <input className={inputCls} placeholder="name (e.g. ci-runner)" value={name} onChange={(e) => setName(e.target.value)} required />
        <input className={inputCls} placeholder="description (optional)" value={description} onChange={(e) => setDescription(e.target.value)} />
        <button type="submit" disabled={create.isPending} className="bg-primary text-primary-foreground px-4 py-1.5 rounded text-sm disabled:opacity-50 hover:opacity-90 transition-opacity">
          {create.isPending ? "Creating…" : "Create"}
        </button>
        {create.error && <div className="md:col-span-3 text-sm text-destructive">{(create.error as Error).message}</div>}
      </form>

      {created && (
        <div className="bg-success/10 border border-success/40 rounded-lg p-3 text-sm space-y-1 text-foreground">
          <div className="font-medium">Key created; copy it now, it won't be shown again.</div>
          <div>name: <span className="mono">{created.name}</span></div>
          <div>value: <code className="bg-card px-2 py-1 rounded border border-border mono">{created.plaintext}</code></div>
          <div className="flex items-center gap-3">
            <button type="button" onClick={copyKey} className="text-success underline text-xs">
              {copyState === "copied" ? "Copied!" : copyState === "failed" ? "Copy failed; select manually" : "Copy to clipboard"}
            </button>
            <button type="button" onClick={() => setCreated(null)} className="text-muted-foreground hover:text-foreground text-xs">
              Dismiss
            </button>
          </div>
        </div>
      )}

      <div className="bg-card text-card-foreground border border-border rounded-lg overflow-hidden">
        <table className="w-full text-sm">
          <thead className="bg-muted/50 text-muted-foreground">
            <tr>
              <th className="text-left px-3 py-2 font-medium">Name</th>
              <th className="text-left px-3 py-2 font-medium">Description</th>
              <th className="text-left px-3 py-2 font-medium">Created</th>
              <th className="text-left px-3 py-2 font-medium">Last used</th>
              <th className="text-right px-3 py-2"></th>
            </tr>
          </thead>
          <tbody>
            {list.data?.keys?.map((k) => (
              <tr key={k.id} className="border-t border-border">
                <td className="px-3 py-1.5">{k.name}</td>
                <td className="px-3 py-1.5 text-muted-foreground">{k.description ?? ""}</td>
                <td className="px-3 py-1.5 text-muted-foreground mono">{new Date(k.created_at).toLocaleString()}</td>
                <td className="px-3 py-1.5 text-muted-foreground mono">{k.last_used_at ? new Date(k.last_used_at).toLocaleString() : "never"}</td>
                <td className="px-3 py-1.5 text-right">
                  <button onClick={() => { if (confirm(`Revoke ${k.name}?`)) remove.mutate(k.name); }} className="text-destructive hover:underline text-xs">Revoke</button>
                </td>
              </tr>
            ))}
            {list.data && (list.data.keys ?? []).length === 0 && (
              <tr><td colSpan={5} className="px-3 py-6 text-center text-muted-foreground">No keys yet.</td></tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}

const inputCls = "bg-background border border-input rounded px-2 py-1 text-sm focus:outline-none focus:ring-2 focus:ring-ring";
