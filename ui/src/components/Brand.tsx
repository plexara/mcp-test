import { useQuery } from "@tanstack/react-query";
import { portalAPI } from "@/lib/api";

// Logo lives in /ui/public; Vite serves it at <base>/plexara-mark.svg.
// With base="/portal/" that resolves to "/portal/plexara-mark.svg".
const MARK = `${import.meta.env.BASE_URL}plexara-mark.svg`;
const WORDMARK = `${import.meta.env.BASE_URL}plexara-wordmark.svg`;

// SidebarBrand is the top-of-sidebar lockup: mark + product name.
export function SidebarBrand() {
  const v = useVersion();
  return (
    <div className="flex items-start gap-2.5 mb-6">
      <img
        src={MARK}
        alt="Plexara"
        className="size-8 shrink-0 mt-0.5"
        draggable={false}
      />
      <div className="min-w-0">
        <div className="font-semibold leading-tight">mcp-test</div>
        <div className="text-xs text-muted-foreground mono truncate" title={v}>
          {v}
        </div>
      </div>
    </div>
  );
}

// SponsoredBy is the small footer line: "Sponsored by [Plexara wordmark]".
export function SponsoredBy() {
  return (
    <a
      href="https://plexara.io"
      target="_blank"
      rel="noopener noreferrer"
      className="group flex items-center justify-between gap-2 rounded-md px-2 py-1.5 text-xs text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
      title="Sponsored by Plexara"
    >
      <span>Sponsored by</span>
      <img
        src={WORDMARK}
        alt="Plexara"
        className="h-3.5 w-auto opacity-70 group-hover:opacity-100 transition-opacity"
        draggable={false}
      />
    </a>
  );
}

// useVersion fetches /api/v1/portal/server once and exposes the build version.
function useVersion(): string {
  const q = useQuery({
    queryKey: ["server-version"],
    queryFn: portalAPI.server,
    staleTime: 60_000,
  });
  return q.data?.version ?? "-";
}
