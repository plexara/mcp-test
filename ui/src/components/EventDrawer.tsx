import { useEffect, useRef, useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { X, Play, GitCompare, AlertCircle, CheckCircle2, Loader2 } from "lucide-react";
import { portalAPI, type AuditEvent, type ReplayResponse, HttpError } from "@/lib/api";
import { JsonView } from "./JsonView";
import { hasRedactedValue, ConfirmModal } from "./ConfirmModal";

type Tab = "overview" | "request" | "response" | "notifications";

// EventDrawer is the audit-row click-to-expand panel. Slides in from the
// right; closes via the X button, the backdrop, or the Escape key. Four
// tabs: Overview / Request / Response / Notifications. The replay
// button calls POST /audit/events/{id}/replay; the Compare-to picker
// stashes this event id in localStorage so the Audit page can wire a
// "compare to last selected" link.
export function EventDrawer({
  eventId,
  onClose,
  onCompareSelect,
}: {
  eventId: string | null;
  onClose: () => void;
  onCompareSelect: (id: string) => void;
}) {
  const [tab, setTab] = useState<Tab>("overview");
  const [confirmReplay, setConfirmReplay] = useState(false);
  const qc = useQueryClient();
  const closeBtnRef = useRef<HTMLButtonElement>(null);
  const restoreFocusRef = useRef<HTMLElement | null>(null);

  // ESC closes.
  useEffect(() => {
    if (!eventId) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [eventId, onClose]);

  // Reset to overview when the selected event changes.
  useEffect(() => {
    if (eventId) setTab("overview");
  }, [eventId]);

  // Save the previously-focused element on open and restore it on close.
  // Auto-focus the close button so keyboard users can dismiss with Enter.
  useEffect(() => {
    if (!eventId) return;
    restoreFocusRef.current = document.activeElement as HTMLElement | null;
    closeBtnRef.current?.focus();
    return () => {
      restoreFocusRef.current?.focus?.();
      restoreFocusRef.current = null;
    };
  }, [eventId]);

  const detail = useQuery<AuditEvent, HttpError>({
    queryKey: ["audit-event", eventId],
    queryFn: () => portalAPI.auditEvent(eventId!),
    enabled: !!eventId,
  });

  // The mutation takes the event id as a variable (rather than closing
  // over the outer `eventId`) so we can: (a) tell at render time which
  // event the in-flight replay was fired for via `replay.variables`,
  // and (b) suppress the banner when the user has navigated to a
  // different drawer mid-flight. Without the variable, a replay fired
  // on event A would resolve into the open drawer for event B and
  // mislead the operator into thinking they replayed B.
  const replay = useMutation<ReplayResponse, HttpError, string>({
    mutationFn: (id: string) => portalAPI.auditReplay(id),
    onSuccess: () => {
      // Refresh the events list so the new replay row appears.
      void qc.invalidateQueries({ queryKey: ["audit"] });
    },
  });

  // Reset replay state on event change so a banner from a prior replay
  // doesn't bleed into a different drawer. Skip the reset while a replay
  // is in flight so we don't clobber an isPending state and mislead the
  // user into thinking nothing's running.
  useEffect(() => {
    if (!replay.isPending) replay.reset();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [eventId]);

  if (!eventId) return null;

  const ev = detail.data;
  // Replay is impossible when the original payload wasn't captured or
  // any param was redacted; mirror the server-side validation client-
  // side so a click doesn't even attempt the request (and so the
  // disabled button telegraphs "this row can't be replayed").
  const replayBlockReason = (() => {
    if (!ev) return "Loading event detail...";
    if (!ev.payload?.request_params) return "No captured request payload to replay.";
    if (hasRedactedValue(ev.payload.request_params)) return "Captured params contain redacted values; replay would not exercise the same call.";
    return null;
  })();

  return (
    <>
      {/* Backdrop */}
      <div
        className="fixed inset-0 bg-black/30 z-40"
        onClick={onClose}
        aria-hidden
      />
      {/* Panel */}
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Audit event detail"
        className="fixed top-0 right-0 h-full w-full max-w-2xl bg-card border-l border-border z-50 flex flex-col shadow-xl"
      >
        <header className="flex items-center justify-between gap-3 px-4 py-3 border-b border-border">
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2">
              {detail.isLoading ? (
                <Loader2 className="size-4 text-muted-foreground shrink-0 animate-spin" />
              ) : detail.isError ? (
                <AlertCircle className="size-4 text-destructive shrink-0" />
              ) : ev?.success === false ? (
                <AlertCircle className="size-4 text-destructive shrink-0" />
              ) : (
                <CheckCircle2 className="size-4 text-success shrink-0" />
              )}
              <h2 className="text-base font-semibold truncate">
                {detail.isError ? "Failed to load event" : ev?.tool_name ?? "Loading..."}
              </h2>
              {ev?.source && (
                <span className="text-xs px-1.5 py-0.5 rounded bg-muted text-muted-foreground">
                  {ev.source}
                </span>
              )}
            </div>
            <div className="text-xs text-muted-foreground mono mt-0.5 truncate" title={eventId}>
              {eventId}
            </div>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={() => onCompareSelect(eventId)}
              className="text-xs px-2 py-1 rounded border border-border hover:bg-muted flex items-center gap-1"
              title="Stash this event for comparison"
            >
              <GitCompare className="size-3.5" /> Compare
            </button>
            <button
              onClick={() => setConfirmReplay(true)}
              disabled={replay.isPending || !!replayBlockReason}
              className="text-xs px-2 py-1 rounded border border-border hover:bg-muted disabled:opacity-50 disabled:cursor-not-allowed flex items-center gap-1"
              title={replayBlockReason ?? "Re-invoke this captured tool call"}
            >
              <Play className="size-3.5" /> {replay.isPending ? "Replaying..." : "Replay"}
            </button>
            <button
              ref={closeBtnRef}
              onClick={onClose}
              className="p-1 text-muted-foreground hover:text-foreground"
              aria-label="Close"
            >
              <X className="size-4" />
            </button>
          </div>
        </header>

        <ConfirmModal
          open={confirmReplay}
          title="Re-invoke this tool call?"
          message={
            <>
              <p>
                Replay re-runs the captured request through the live MCP server with the
                <strong> same arguments</strong>. Any side effect the original call had
                (database writes, outbound API calls, billable operations) will fire again.
              </p>
              {ev?.tool_name && (
                <p className="mt-2 mono text-xs text-muted-foreground">
                  Tool: <span className="text-foreground">{ev.tool_name}</span>
                </p>
              )}
              <p className="mt-2 text-xs text-muted-foreground">
                Replay re-runs side effects. Treat like Try-It, not a production self-service.
              </p>
            </>
          }
          confirmLabel="Replay"
          danger
          onConfirm={() => {
            setConfirmReplay(false);
            replay.mutate(eventId);
          }}
          onCancel={() => setConfirmReplay(false)}
        />

        {/* Suppress the banner when the in-flight replay was fired
            against a different event (user navigated away mid-flight).
            replay.variables is undefined in idle state and equal to the
            event id passed to mutate() while pending or settled. */}
        {(!replay.variables || replay.variables === eventId) && (
          <ReplayBanner replay={replay} />
        )}

        <nav className="flex border-b border-border text-sm">
          {(["overview", "request", "response", "notifications"] as Tab[]).map((t) => (
            <button
              key={t}
              onClick={() => setTab(t)}
              className={`px-4 py-2 border-b-2 -mb-px transition-colors ${
                tab === t
                  ? "border-primary text-foreground"
                  : "border-transparent text-muted-foreground hover:text-foreground"
              }`}
            >
              {t}
              {t === "notifications" && ev?.payload?.notifications?.length
                ? ` (${ev.payload.notifications.length}${ev.payload.notifications_truncated ? "+" : ""})`
                : ""}
            </button>
          ))}
        </nav>

        <div className="flex-1 overflow-auto p-4">
          {detail.isLoading && <div className="text-muted-foreground text-sm">Loading...</div>}
          {detail.isError && (
            <div className="text-destructive text-sm">
              {detail.error.message}
            </div>
          )}
          {ev && tab === "overview" && <OverviewTab ev={ev} />}
          {ev && tab === "request" && <RequestTab ev={ev} />}
          {ev && tab === "response" && <ResponseTab ev={ev} />}
          {ev && tab === "notifications" && <NotificationsTab ev={ev} />}
        </div>
      </div>
    </>
  );
}

function ReplayBanner({
  replay,
}: {
  replay: ReturnType<typeof useMutation<ReplayResponse, HttpError, string>>;
}) {
  if (replay.isError) {
    return (
      <div className="bg-destructive/10 text-destructive border-b border-destructive/30 px-4 py-2 text-sm flex items-start gap-2">
        <AlertCircle className="size-4 mt-0.5 shrink-0" />
        <div className="min-w-0">
          <div className="font-medium">Replay failed</div>
          <div className="text-xs">{replay.error.message}</div>
        </div>
      </div>
    );
  }
  if (replay.data) {
    return (
      <div className={`px-4 py-2 text-sm border-b flex items-start gap-2 ${
        replay.data.success
          ? "bg-success/10 text-success border-success/30"
          : "bg-destructive/10 text-destructive border-destructive/30"
      }`}>
        {replay.data.success ? (
          <CheckCircle2 className="size-4 mt-0.5 shrink-0" />
        ) : (
          <AlertCircle className="size-4 mt-0.5 shrink-0" />
        )}
        <div className="min-w-0 flex-1">
          <div className="font-medium">
            {replay.data.success ? "Replay succeeded" : "Replay returned tool error"}
          </div>
          <div className="text-xs mono break-all">
            new event:{" "}
            <Link
              to={`/audit?id=${replay.data.replay_event_id}`}
              className="underline hover:no-underline"
            >
              {replay.data.replay_event_id}
            </Link>
          </div>
        </div>
      </div>
    );
  }
  return null;
}

function OverviewTab({ ev }: { ev: AuditEvent }) {
  return (
    <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-2 text-sm">
      <Row k="When" v={new Date(ev.timestamp).toLocaleString()} />
      <Row k="Duration" v={`${ev.duration_ms} ms`} />
      <Row k="Tool" v={ev.tool_name} />
      {ev.tool_group && <Row k="Tool group" v={ev.tool_group} />}
      <Row k="Result" v={
        ev.success ? "ok" : (
          <span className="text-destructive">
            {ev.error_category ?? "error"}
            {ev.error_message ? ` — ${ev.error_message}` : ""}
          </span>
        )
      } />
      <Row k="Source" v={ev.source} />
      <Row k="Transport" v={ev.transport} />
      {ev.request_id && <Row k="Request ID" v={<span className="mono break-all">{ev.request_id}</span>} />}
      {ev.session_id && <Row k="Session ID" v={<span className="mono break-all">{ev.session_id}</span>} />}
      {ev.user_subject && <Row k="User subject" v={<span className="mono break-all">{ev.user_subject}</span>} />}
      {ev.user_email && <Row k="User email" v={ev.user_email} />}
      {ev.auth_type && <Row k="Auth type" v={ev.auth_type} />}
      {ev.api_key_name && <Row k="API key" v={ev.api_key_name} />}
      {ev.remote_addr && <Row k="Remote addr" v={<span className="mono">{ev.remote_addr}</span>} />}
      {ev.user_agent && <Row k="User agent" v={<span className="mono break-all text-xs">{ev.user_agent}</span>} />}
      {(ev.request_chars ?? 0) > 0 && <Row k="Request chars" v={String(ev.request_chars)} />}
      {(ev.response_chars ?? 0) > 0 && <Row k="Response chars" v={String(ev.response_chars)} />}
      {(ev.content_blocks ?? 0) > 0 && <Row k="Content blocks" v={String(ev.content_blocks)} />}
      {ev.payload?.replayed_from && (
        <Row k="Replayed from" v={
          <Link to={`/audit?id=${ev.payload.replayed_from}`} className="mono break-all underline">
            {ev.payload.replayed_from}
          </Link>
        } />
      )}
    </dl>
  );
}

function RequestTab({ ev }: { ev: AuditEvent }) {
  const params = ev.payload?.request_params ?? ev.parameters;
  const headers = ev.payload?.request_headers;
  const noPayload = !ev.payload;
  return (
    <div className="space-y-4">
      {ev.payload?.request_truncated && (
        <div className="text-xs text-warning bg-warning/10 border border-warning/30 rounded px-2 py-1">
          Request payload was truncated at storage time (exceeded max_payload_bytes).
        </div>
      )}
      {noPayload && !params && (
        <div className="text-sm text-muted-foreground italic">
          No request captured. Either capture_payloads is off for this deployment, or this row
          predates payload capture (events written before v1.1.0).
        </div>
      )}
      {noPayload && params && (
        <div className="text-xs text-muted-foreground italic">
          Showing summary parameters only; full payload was not captured for this row.
        </div>
      )}
      {params && <JsonView label="Parameters" value={params} />}
      {headers && Object.keys(headers).length > 0 && (
        <JsonView label="Request headers" value={headers} />
      )}
    </div>
  );
}

function ResponseTab({ ev }: { ev: AuditEvent }) {
  const result = ev.payload?.response_result;
  const err = ev.payload?.response_error;
  return (
    <div className="space-y-4">
      {ev.payload?.response_truncated && (
        <div className="text-xs text-warning bg-warning/10 border border-warning/30 rounded px-2 py-1">
          Response payload was truncated at storage time.
        </div>
      )}
      {err && <JsonView label="response_error" value={err} />}
      {result && <JsonView label="response_result" value={result} />}
      {!err && !result && (
        <div className="text-sm text-muted-foreground italic">
          No response captured. Either capture_payloads is off for this deployment, or this row
          predates payload capture (events written before v1.1.0).
        </div>
      )}
    </div>
  );
}

function NotificationsTab({ ev }: { ev: AuditEvent }) {
  const ns = ev.payload?.notifications ?? [];
  if (ns.length === 0) {
    return (
      <div className="text-sm text-muted-foreground italic">
        No notifications were dispatched during this call.
      </div>
    );
  }
  return (
    <div className="space-y-3">
      {ev.payload?.notifications_truncated && (
        <div className="text-xs text-warning bg-warning/10 border border-warning/30 rounded px-2 py-1">
          Notification list was trimmed to fit max_payload_bytes; trailing entries are missing.
        </div>
      )}
      <ol className="space-y-3">
        {ns.map((n, i) => (
          <li key={i} className="border border-border rounded p-2">
            <div className="flex justify-between text-xs text-muted-foreground mono mb-1">
              <span>{n.method}</span>
              <span>{new Date(n.ts).toLocaleTimeString()}</span>
            </div>
            <JsonView value={n.params ?? {}} />
          </li>
        ))}
      </ol>
    </div>
  );
}

function Row({ k, v }: { k: string; v: React.ReactNode }) {
  return (
    <>
      <dt className="text-muted-foreground">{k}</dt>
      <dd className="min-w-0">{v}</dd>
    </>
  );
}
