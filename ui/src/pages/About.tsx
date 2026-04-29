import { useQuery } from "@tanstack/react-query";
import { portalAPI } from "@/lib/api";
import { ExternalLink } from "lucide-react";

const MARK = `${import.meta.env.BASE_URL}plexara-mark.svg`;

export default function About() {
  const server = useQuery({ queryKey: ["server"], queryFn: portalAPI.server });
  const tools = useQuery({ queryKey: ["tools"], queryFn: portalAPI.tools });

  const groups = (tools.data?.tools ?? []).reduce<Record<string, number>>((acc, t) => {
    acc[t.group] = (acc[t.group] ?? 0) + 1;
    return acc;
  }, {});

  return (
    <div className="space-y-8 max-w-3xl">
      <div className="flex items-start gap-4">
        <img src={MARK} alt="Plexara" className="size-12 shrink-0 mt-1" draggable={false} />
        <div>
          <h1 className="text-2xl font-semibold">About mcp-test</h1>
          <p className="text-muted-foreground mt-1">
            A controllable MCP server for exercising MCP gateways end-to-end.
          </p>
        </div>
      </div>

      <Section title="What this is">
        <p>
          <span className="font-medium text-foreground">mcp-test</span> is a Go
          implementation of the{" "}
          <a className="text-primary hover:underline"
             href="https://modelcontextprotocol.io/" target="_blank" rel="noreferrer">
            Model Context Protocol
          </a>{" "}
          built specifically as a fixture for testing MCP gateways. The tools
          it exposes are intentionally simple, deterministic, and observable.
          The point isn't what they <em>do</em>; it's how they let you verify
          the gateway in front of them is doing the right things: forwarding
          identity, redacting secrets, enforcing rate limits, applying
          enrichment, passing through progress notifications, surviving
          failures, and so on.
        </p>
        <p>
          It is also an opinionated reference for building production-quality
          MCP servers in Go (official SDK, streamable HTTP transport, OIDC
          delegation, Postgres-backed audit log, embedded React portal).
        </p>
      </Section>

      <Section title="Why it exists">
        <p>
          <a className="text-primary hover:underline inline-flex items-center gap-1"
             href="https://plexara.io" target="_blank" rel="noopener noreferrer">
            Plexara <ExternalLink className="size-3" />
          </a>{" "}
          is a commercial MCP server: a data platform that connects AI agents
          to your enterprise data, with{" "}
          <span className="font-medium text-foreground">gateway capabilities
          built in</span>. The same server that exposes tools also enforces
          identity, applies{" "}
          <span className="font-medium text-foreground">configurable
          enrichment</span> (context injection, redaction, response shaping,
          dedup, caching) on the way in and out, and writes a tamper-evident
          audit trail of everything it brokers.
        </p>
        <p>
          Validating that gateway behavior end-to-end needs a predictable
          counterpart on the wire. That's{" "}
          <span className="font-medium text-foreground">mcp-test</span>. Point
          a Plexara MCP client at it, configure an enrichment rule on the
          Plexara side, and compare what your client sees against what
          mcp-test's audit log recorded; the diff is the gateway's behavior,
          made observable.
        </p>
      </Section>

      <Section title="Tool categories">
        <p>
          Tools are grouped by what they help you test, not by what they
          compute:
        </p>
        <ul className="space-y-2">
          <ToolCategory
            name="identity"
            count={groups.identity ?? 0}
            blurb="whoami, echo, headers; verify the gateway is forwarding identity, args, and headers (and redacting the right ones)."
          />
          <ToolCategory
            name="data"
            count={groups.data ?? 0}
            blurb="fixed_response, sized_response, lorem; deterministic outputs for testing enrichment dedup, response-size limits, and caching boundaries."
          />
          <ToolCategory
            name="failure"
            count={groups.failure ?? 0}
            blurb="error, slow, flaky; controlled failure modes (errors, latency, probabilistic flakiness) for testing retry and timeout policies."
          />
          <ToolCategory
            name="streaming"
            count={groups.streaming ?? 0}
            blurb="progress, long_output, chatty; progress notifications and multi-block content for testing streamable-http behaviour end-to-end."
          />
        </ul>
      </Section>

      <Section title="Build">
        <div className="grid grid-cols-[8rem_1fr] gap-y-1 text-sm">
          <span className="text-muted-foreground">Version</span>
          <span className="mono">{server.data?.version ?? "-"}</span>
          <span className="text-muted-foreground">Commit</span>
          <span className="mono">{server.data?.commit ?? "-"}</span>
          <span className="text-muted-foreground">Built</span>
          <span className="mono">{server.data?.date ?? "-"}</span>
        </div>
      </Section>

      <Section title="Sponsor">
        <p>
          mcp-test is open-source and sponsored by{" "}
          <a className="text-primary hover:underline inline-flex items-center gap-1"
             href="https://plexara.io" target="_blank" rel="noopener noreferrer">
            plexara.io <ExternalLink className="size-3" />
          </a>
          . If you're building MCP integrations for an enterprise data stack -
          governance, observability, enrichment, multi-tenant routing; that's
          what Plexara is for.
        </p>
      </Section>
    </div>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="space-y-3">
      <h2 className="text-lg font-medium">{title}</h2>
      <div className="prose prose-sm dark:prose-invert max-w-none text-foreground/90 space-y-3">
        {children}
      </div>
    </section>
  );
}

function ToolCategory({ name, count, blurb }: { name: string; count: number; blurb: string }) {
  return (
    <li className="bg-card border border-border rounded-md p-3">
      <div className="flex items-baseline gap-2 mb-0.5">
        <span className="font-medium mono">{name}</span>
        <span className="text-xs text-muted-foreground">{count} {count === 1 ? "tool" : "tools"}</span>
      </div>
      <div className="text-sm text-muted-foreground">{blurb}</div>
    </li>
  );
}
