import { useState } from "react";
import { setApiKey } from "@/lib/api";
import { useAuth } from "@/stores/auth";
import { useNavigate } from "react-router-dom";
import ThemeToggle from "@/components/ThemeToggle";
import { SponsoredBy } from "@/components/Brand";

const MARK = `${import.meta.env.BASE_URL}plexara-mark.svg`;

export default function Login() {
  const [key, setKey] = useState("");
  const [error, setError] = useState<string | null>(null);
  const refresh = useAuth((s) => s.refresh);
  const navigate = useNavigate();

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setApiKey(key.trim());
    await refresh();
    if (useAuth.getState().status === "authenticated") {
      navigate("/", { replace: true });
    } else {
      setError("API key was not accepted.");
    }
  };

  return (
    <div className="min-h-full grid place-items-center bg-background p-6 relative">
      <div className="absolute top-4 right-4">
        <ThemeToggle />
      </div>
      <div className="w-full max-w-sm bg-card text-card-foreground rounded-lg shadow-sm border border-border p-6 space-y-4">
        <div className="flex items-start gap-3">
          <img src={MARK} alt="Plexara" className="size-10 shrink-0 mt-0.5" draggable={false} />
          <div>
            <div className="text-xl font-semibold">mcp-test portal</div>
            <div className="text-sm text-muted-foreground">Sign in to inspect tools and audit logs.</div>
          </div>
        </div>

        <a
          href="/portal/auth/login"
          className="block w-full text-center bg-primary text-primary-foreground py-2 rounded hover:opacity-90 transition-opacity"
        >
          Sign in with OIDC
        </a>

        <div className="relative">
          <div className="absolute inset-0 flex items-center">
            <div className="w-full border-t border-border" />
          </div>
          <div className="relative bg-card px-2 text-xs text-muted-foreground text-center w-fit mx-auto">
            or use an API key
          </div>
        </div>

        <form onSubmit={submit} className="space-y-3">
          <input
            type="password"
            placeholder="X-API-Key"
            value={key}
            onChange={(e) => setKey(e.target.value)}
            className="w-full bg-background border border-input rounded px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
          />
          {error && <div className="text-sm text-destructive">{error}</div>}
          <button
            type="submit"
            className="w-full bg-card border border-input text-foreground py-2 rounded hover:bg-muted transition-colors"
          >
            Sign in with API key
          </button>
        </form>

        <div className="pt-2 border-t border-border">
          <SponsoredBy />
        </div>
      </div>
    </div>
  );
}
