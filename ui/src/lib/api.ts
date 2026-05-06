// Thin fetch wrapper that:
//   - Targets /api/v1/portal/* and /api/v1/admin/* on the same origin.
//   - Sends X-API-Key from sessionStorage when present (falls through to the
//     session cookie otherwise).
//   - Always sends X-Requested-With on writes so the server's CSRF check
//     accepts the request (and a forged <form> POST cannot reach it).
//   - Surfaces 401 as a typed Error and triggers a registered handler so
//     the auth store can clear local state and bounce to /login.

const API_KEY_STORAGE = "mcp-test-api-key";

export class HttpError extends Error {
  constructor(public status: number, message: string, public body?: unknown) {
    super(message);
  }
}

export function setApiKey(key: string) {
  sessionStorage.setItem(API_KEY_STORAGE, key);
}

export function clearApiKey() {
  sessionStorage.removeItem(API_KEY_STORAGE);
}

export function getApiKey(): string | null {
  return sessionStorage.getItem(API_KEY_STORAGE);
}

// onUnauthorized is called when any API request returns 401. The auth store
// registers a handler at startup; until then we no-op (so library tests
// don't blow up).
let onUnauthorized: (() => void) | null = null;
export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn;
}

async function request<T>(
  path: string,
  init: RequestInit = {},
  signal?: AbortSignal,
): Promise<T> {
  const headers = new Headers(init.headers);
  const key = getApiKey();
  if (key) headers.set("X-API-Key", key);
  if (init.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  // The server requires this header on POST/PUT/PATCH/DELETE as a CSRF
  // mitigation; cheap to send on every request.
  if (!headers.has("X-Requested-With")) {
    headers.set("X-Requested-With", "XMLHttpRequest");
  }
  const resp = await fetch(path, {
    credentials: "include",
    ...init,
    headers,
    signal,
  });
  if (resp.status === 204) return undefined as T;

  // If the server returns HTML (e.g. a 502 from a misconfigured proxy),
  // don't try to JSON.parse the entire page; surface a stable message.
  const ct = resp.headers.get("content-type") || "";
  let body: unknown;
  if (ct.includes("application/json")) {
    body = await resp.json().catch(() => undefined);
  } else {
    const text = await resp.text();
    body = text;
  }

  if (!resp.ok) {
    if (resp.status === 401) {
      clearApiKey();
      onUnauthorized?.();
    }
    const msg =
      typeof body === "object" && body !== null && "error" in body
        ? String((body as { error: string }).error)
        : resp.statusText || `HTTP ${resp.status}`;
    throw new HttpError(resp.status, msg, body);
  }
  return body as T;
}

export const api = {
  get:    <T>(path: string, signal?: AbortSignal) => request<T>(path, undefined, signal),
  post:   <T>(path: string, body: unknown, signal?: AbortSignal) => request<T>(path, { method: "POST", body: JSON.stringify(body) }, signal),
  delete: <T>(path: string, signal?: AbortSignal) => request<T>(path, { method: "DELETE" }, signal),
};

// --- typed endpoints ---

export type Identity = {
  subject: string;
  email?: string;
  name?: string;
  auth_type: "oidc" | "apikey" | "anonymous";
  claims?: Record<string, unknown>;
  api_key_id?: string;
};

export type ToolMeta = {
  name: string;
  group: string;
  description: string;
  input_schema?: unknown;
};

export type AuditEvent = {
  id: string;
  timestamp: string;
  duration_ms: number;
  request_id?: string;
  session_id?: string;
  user_subject?: string;
  user_email?: string;
  auth_type?: string;
  api_key_name?: string;
  tool_name: string;
  tool_group?: string;
  parameters?: Record<string, unknown>;
  success: boolean;
  error_message?: string;
  error_category?: string;
  request_chars?: number;
  response_chars?: number;
  content_blocks?: number;
  transport: string;
  source: string;
  remote_addr?: string;
  user_agent?: string;
  payload?: AuditPayload;
};

export type AuditNotification = {
  ts: string;
  method: string;
  params?: Record<string, unknown>;
};

export type AuditPayload = {
  jsonrpc_method?: string;
  request_params?: Record<string, unknown>;
  request_size_bytes?: number;
  request_truncated?: boolean;
  request_headers?: Record<string, string[]>;
  request_remote_addr?: string;
  response_result?: Record<string, unknown>;
  response_error?: Record<string, unknown>;
  response_size_bytes?: number;
  response_truncated?: boolean;
  notifications?: AuditNotification[];
  notifications_truncated?: boolean;
  replayed_from?: string;
};

export type ReplayResponse = {
  replay_event_id: string;
  replayed_from: string;
  result: unknown;
  success: boolean;
  error?: string;
};

export type AuditMeta = {
  has_keys: string[];
  json_sources: string[];
  replay: { burst: number; refill_secs: number; sustained_min: number };
  export: { max_rows: number };
};

export type DashboardResponse = {
  window_from: string;
  window_to: string;
  stats: {
    total: number;
    errors: number;
    error_rate: number;
    avg_duration_ms: number;
    p50_duration_ms: number;
    p95_duration_ms: number;
    unique_subjects: number;
    unique_tools: number;
  };
  recent: AuditEvent[];
};

export type Key = {
  id: string;
  name: string;
  description?: string;
  created_by?: string;
  created_at: string;
  expires_at?: string;
  last_used_at?: string;
};

export const portalAPI = {
  me:        () => api.get<Identity>("/api/v1/portal/me"),
  server:    () => api.get<{ version: string; commit: string; date: string; config: unknown }>("/api/v1/portal/server"),
  instructions: () => api.get<{ instructions: string }>("/api/v1/portal/instructions"),
  tools:     () => api.get<{ tools: ToolMeta[] }>("/api/v1/portal/tools"),
  toolDetail: (name: string) => api.get<ToolMeta>(`/api/v1/portal/tools/${encodeURIComponent(name)}`),
  auditMeta: () => api.get<AuditMeta>("/api/v1/portal/audit/meta"),
  audit:     (qs: string) => api.get<{ events: AuditEvent[]; total: number; limit: number; offset: number }>(`/api/v1/portal/audit/events${qs ? "?" + qs : ""}`),
  auditEvent: (id: string) => api.get<AuditEvent>(`/api/v1/portal/audit/events/${encodeURIComponent(id)}`),
  auditReplay: (id: string) => api.post<ReplayResponse>(`/api/v1/portal/audit/events/${encodeURIComponent(id)}/replay`, {}),
  dashboard: () => api.get<DashboardResponse>("/api/v1/portal/dashboard"),
  wellknown: () => api.get<{ protected_resource_url: string; authorization_server: string; oidc_enabled: boolean; audience: string; mcp_endpoint: string }>("/api/v1/portal/wellknown"),
};

// streamAuditEvents opens a fetch-based SSE connection to the live tail
// endpoint and invokes onEvent per `event: audit` frame. Returns an
// unsubscribe function that aborts the request.
//
// Uses fetch + ReadableStream rather than EventSource because EventSource
// can't send custom headers (X-API-Key); cookie-only auth would lock out
// CLI / API-key callers. SSE comments (": connected", ": keepalive") are
// silently skipped.
export function streamAuditEvents(
  onEvent: (ev: AuditEvent) => void,
  onError?: (err: Error) => void,
): () => void {
  const ctrl = new AbortController();
  void (async () => {
    try {
      const headers = new Headers();
      const key = getApiKey();
      if (key) headers.set("X-API-Key", key);
      headers.set("Accept", "text/event-stream");
      const resp = await fetch("/api/v1/portal/audit/stream", {
        credentials: "include",
        headers,
        signal: ctrl.signal,
      });
      if (resp.status === 401) {
        // Auth handler will redirect to /login; suppress the per-stream
        // error callback so a flash banner doesn't precede the redirect.
        clearApiKey();
        onUnauthorized?.();
        return;
      }
      if (!resp.ok || !resp.body) {
        throw new HttpError(resp.status, `stream open failed: HTTP ${resp.status}${resp.statusText ? " " + resp.statusText : ""}`);
      }
      const reader = resp.body.getReader();
      const decoder = new TextDecoder();
      // Cap the line buffer so a misbehaving producer that never emits
      // a newline can't grow it without bound and OOM the tab.
      const maxBuf = 1 << 20; // 1 MiB
      let buf = "";
      let event = "";
      let data = "";
      while (true) {
        const { done, value } = await reader.read();
        if (done) {
          // The server closed the stream (heartbeat write failure, idle
          // timeout, or shutdown). Surface so the UI can re-enable the tail.
          throw new Error("stream closed by server");
        }
        buf += decoder.decode(value, { stream: true });
        if (buf.length > maxBuf) {
          throw new Error("stream buffer overflow (no newline within 1 MiB)");
        }
        let nl: number;
        while ((nl = buf.indexOf("\n")) >= 0) {
          const raw = buf.slice(0, nl);
          buf = buf.slice(nl + 1);
          const line = raw.endsWith("\r") ? raw.slice(0, -1) : raw;
          if (line === "") {
            if (event === "audit" && data) {
              try {
                const parsed = JSON.parse(data) as AuditEvent;
                if (parsed && typeof parsed.id === "string" && parsed.id) {
                  onEvent(parsed);
                }
              } catch {
                // Malformed event payload — skip and keep reading.
              }
            }
            event = "";
            data = "";
            continue;
          }
          if (line.startsWith(":")) continue; // SSE comment
          if (line.startsWith("event:")) {
            event = line.slice(6).replace(/^ /, "");
          } else if (line.startsWith("data:")) {
            // SSE: strip exactly one optional leading space; preserve any other whitespace.
            const fragment = line.slice(5).replace(/^ /, "");
            data = data ? data + "\n" + fragment : fragment;
          }
        }
      }
    } catch (err) {
      if ((err as Error).name === "AbortError") return;
      onError?.(err as Error);
    }
  })();
  return () => ctrl.abort();
}

export const adminAPI = {
  listKeys:   () => api.get<{ keys: Key[] }>("/api/v1/admin/keys"),
  createKey:  (name: string, description?: string) => api.post<{ key: Key; plaintext: string }>("/api/v1/admin/keys", { name, description }),
  deleteKey:  (name: string) => api.delete<void>(`/api/v1/admin/keys/${encodeURIComponent(name)}`),
  tryit:      (name: string, args: Record<string, unknown>) =>
    api.post<{ content: { type: string; text?: string }[]; structuredContent?: unknown; isError?: boolean }>(
      `/api/v1/admin/tryit/${encodeURIComponent(name)}`,
      { arguments: args },
    ),
};
