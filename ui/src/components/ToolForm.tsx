import { useEffect, useMemo, useRef, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { adminAPI } from "@/lib/api";

// Field is a tagged union for the per-tool form specs below.
type Field =
  | { kind: "text";     name: string; label: string; help?: string; default?: string; placeholder?: string; required?: boolean }
  | { kind: "number";   name: string; label: string; help?: string; default?: number; min?: number; max?: number; step?: number; required?: boolean }
  | { kind: "slider";   name: string; label: string; help?: string; default: number; min: number; max: number; step?: number; format?: (v: number) => string }
  | { kind: "boolean";  name: string; label: string; help?: string; default?: boolean }
  | { kind: "select";   name: string; label: string; help?: string; default?: string; options: { value: string; label: string }[] }
  | { kind: "json";     name: string; label: string; help?: string; default?: string; rows?: number };

// FORMS maps every tool name to its declarative field list. Tools with no
// arguments get an empty list and render a "Run" button only.
const FORMS: Record<string, Field[]> = {
  // identity ----------------------------------------------------------------
  whoami: [],
  headers: [],
  echo: [
    { kind: "text", name: "message", label: "Message",
      help: "A short string echoed back verbatim.",
      default: "hello from try-it" },
    { kind: "json", name: "extras", label: "Extras (JSON object)",
      help: "Optional free-form payload. Pass an object; it will be echoed back as-is.",
      default: `{ "k": "v" }`, rows: 4 },
  ],

  // data --------------------------------------------------------------------
  fixed_response: [
    { kind: "text", name: "key", label: "Key", required: true,
      help: "Same key always yields the same body. Try changing it to see the hash change.",
      default: "hello" },
  ],
  sized_response: [
    { kind: "slider", name: "size", label: "Size (chars)",
      default: 1024, min: 0, max: 65536, step: 16,
      help: "Returns exactly this many bytes of repeating ASCII.",
      format: (v) => `${v.toLocaleString()} chars` },
  ],
  lorem: [
    { kind: "slider", name: "words", label: "Words",
      default: 50, min: 1, max: 5000, step: 1,
      help: "Number of words to generate. Capped at 5000." },
    { kind: "text", name: "seed", label: "Seed (optional)",
      help: "Same seed → same output. Leave empty for non-deterministic." },
  ],

  // failure -----------------------------------------------------------------
  error: [
    { kind: "text", name: "message", label: "Error message",
      default: "synthetic failure",
      help: "The error message returned to the caller." },
    { kind: "select", name: "category", label: "Category",
      default: "tool",
      help: "Audit-log filter label.",
      options: [
        { value: "",         label: "(none)" },
        { value: "protocol", label: "protocol" },
        { value: "tool",     label: "tool" },
        { value: "timeout",  label: "timeout" },
        { value: "auth",     label: "auth" },
      ] },
    { kind: "boolean", name: "as_tool", label: "Tool-level error (IsError=true)",
      default: true,
      help: "If on, returns CallToolResult.IsError=true; otherwise raises a protocol error." },
  ],
  slow: [
    { kind: "slider", name: "milliseconds", label: "Sleep duration",
      default: 500, min: 0, max: 60000, step: 100,
      help: "Capped at 60 seconds. Honors context cancellation.",
      format: (v) => v >= 1000 ? `${(v/1000).toFixed(1)}s` : `${v}ms` },
  ],
  flaky: [
    { kind: "slider", name: "fail_rate", label: "Failure probability",
      default: 0.5, min: 0, max: 1, step: 0.05,
      help: "Probability the call fails. Combined with seed + call_id for reproducibility.",
      format: (v) => `${Math.round(v * 100)}%` },
    { kind: "text", name: "seed", label: "Seed (optional)",
      help: "Same seed + same call_id always yields the same outcome." },
    { kind: "number", name: "call_id", label: "Call ID",
      default: 1, min: 0, step: 1,
      help: "Iteration index. Bump this between calls to get different outcomes with the same seed." },
  ],

  // streaming ---------------------------------------------------------------
  progress: [
    { kind: "slider", name: "steps", label: "Steps",
      default: 5, min: 1, max: 100, step: 1,
      help: "Number of progress notifications to emit." },
    { kind: "slider", name: "step_ms", label: "Delay between steps",
      default: 200, min: 0, max: 5000, step: 50,
      help: "Sleep between notifications.",
      format: (v) => `${v}ms` },
  ],
  long_output: [
    { kind: "slider", name: "blocks", label: "Content blocks",
      default: 3, min: 1, max: 50, step: 1,
      help: "Number of text content blocks in the result." },
    { kind: "slider", name: "chars", label: "Characters per block",
      default: 256, min: 1, max: 65536, step: 32,
      help: "Each block contains this many characters of filler.",
      format: (v) => v.toLocaleString() },
  ],
  chatty: [],
};

// ToolForm renders a per-tool form (or "Run" button if no args), submits the
// args to the admin tryit endpoint, and shows the result. When a tool isn't
// in the FORMS registry, fields are derived from inputSchema so newly-added
// tools at least get a working (if plain) form.
export default function ToolForm({ toolName, inputSchema }: { toolName: string; inputSchema?: unknown }) {
  const fields = FORMS[toolName] ?? fieldsFromSchema(inputSchema);
  const [values, setValues] = useState<Record<string, any>>(() => initialValues(fields));
  const [showRaw, setShowRaw] = useState(false);
  const [rawError, setRawError] = useState<string | null>(null);

  // Reset state when switching tools.
  const lastTool = useRef(toolName);
  useEffect(() => {
    if (lastTool.current !== toolName) {
      lastTool.current = toolName;
      setValues(initialValues(fields));
      setRawError(null);
    }
  }, [toolName, fields]);

  const m = useMutation({
    mutationFn: (args: Record<string, any>) => adminAPI.tryit(toolName, args),
  });

  // The args object that will actually be sent. Strings turn into string,
  // numbers into number, booleans into bool. Empty optional fields are
  // omitted.
  const args = useMemo(() => buildArgs(fields, values), [fields, values]);

  const onSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    setRawError(null);
    m.mutate(args);
  };

  if (fields === null) {
    return (
      <div className="text-sm text-muted-foreground space-y-2">
        <p>
          No form is defined for <code className="mono">{toolName}</code> and no
          input schema was provided.
        </p>
        <button
          type="button"
          onClick={() => m.mutate({})}
          className="bg-primary text-primary-foreground px-4 py-1.5 rounded text-sm hover:opacity-90 transition-opacity"
        >
          Run with empty args
        </button>
      </div>
    );
  }

  return (
    <form onSubmit={onSubmit} className="space-y-4">
      {fields.length === 0 && (
        <p className="text-sm text-muted-foreground">
          This tool takes no arguments.
        </p>
      )}

      {fields.map((f) => (
        <FieldRow
          key={f.name}
          field={f}
          value={values[f.name]}
          onChange={(v) => setValues((s) => ({ ...s, [f.name]: v }))}
        />
      ))}

      <div className="flex items-center justify-between gap-3">
        <button
          type="submit"
          disabled={m.isPending}
          className="bg-primary text-primary-foreground px-4 py-1.5 rounded text-sm disabled:opacity-50 hover:opacity-90 transition-opacity"
        >
          {m.isPending ? "Running…" : "Run"}
        </button>
        <button
          type="button"
          onClick={() => setShowRaw((s) => !s)}
          className="text-xs text-muted-foreground hover:text-foreground"
        >
          {showRaw ? "Hide" : "Show"} raw JSON
        </button>
      </div>

      {showRaw && (
        <div>
          <div className="text-xs text-muted-foreground mb-1">Arguments sent</div>
          <pre className="bg-card border border-border rounded p-2 mono text-xs overflow-auto">
{JSON.stringify(args, null, 2)}
          </pre>
        </div>
      )}

      {rawError && <div className="text-sm text-destructive">{rawError}</div>}
      {m.error && <div className="text-sm text-destructive">{(m.error as Error).message}</div>}

      {m.data && <Result data={m.data} />}
    </form>
  );
}

// FieldRow dispatches a Field to the right input control.
function FieldRow({ field, value, onChange }: {
  field: Field;
  value: any;
  onChange: (v: any) => void;
}) {
  const labelEl = (
    <div className="flex items-baseline justify-between gap-2 mb-1">
      <label className="text-sm font-medium">{field.label}</label>
      {"required" in field && field.required && (
        <span className="text-xs text-muted-foreground">required</span>
      )}
    </div>
  );

  const helpEl = field.help && (
    <div className="text-xs text-muted-foreground mt-1">{field.help}</div>
  );

  switch (field.kind) {
    case "text":
      return (
        <div>
          {labelEl}
          <input
            type="text"
            value={value ?? ""}
            onChange={(e) => onChange(e.target.value)}
            placeholder={field.placeholder}
            required={field.required}
            className={inputCls}
          />
          {helpEl}
        </div>
      );

    case "number":
      return (
        <div>
          {labelEl}
          <input
            type="number"
            value={value ?? ""}
            onChange={(e) => onChange(e.target.value === "" ? "" : Number(e.target.value))}
            min={field.min}
            max={field.max}
            step={field.step}
            required={field.required}
            className={inputCls}
          />
          {helpEl}
        </div>
      );

    case "slider": {
      const display = field.format ? field.format(value) : String(value);
      return (
        <div>
          <div className="flex items-baseline justify-between gap-2 mb-1">
            <label className="text-sm font-medium">{field.label}</label>
            <span className="text-xs mono text-muted-foreground">{display}</span>
          </div>
          <div className="flex items-center gap-3">
            <input
              type="range"
              value={value}
              onChange={(e) => onChange(Number(e.target.value))}
              min={field.min}
              max={field.max}
              step={field.step ?? 1}
              className="flex-1 accent-primary"
            />
            <input
              type="number"
              value={value}
              onChange={(e) => onChange(Number(e.target.value))}
              min={field.min}
              max={field.max}
              step={field.step ?? 1}
              className={`${inputCls} w-24`}
            />
          </div>
          {helpEl}
        </div>
      );
    }

    case "boolean":
      return (
        <div>
          <label className="flex items-center gap-2 text-sm cursor-pointer">
            <input
              type="checkbox"
              checked={!!value}
              onChange={(e) => onChange(e.target.checked)}
              className="rounded border-input accent-primary"
            />
            <span className="font-medium">{field.label}</span>
          </label>
          {helpEl}
        </div>
      );

    case "select":
      return (
        <div>
          {labelEl}
          <select
            value={value ?? ""}
            onChange={(e) => onChange(e.target.value)}
            className={inputCls}
          >
            {field.options.map((o) => (
              <option key={o.value} value={o.value}>{o.label}</option>
            ))}
          </select>
          {helpEl}
        </div>
      );

    case "json":
      return (
        <div>
          {labelEl}
          <textarea
            value={value ?? ""}
            onChange={(e) => onChange(e.target.value)}
            rows={field.rows ?? 4}
            spellCheck={false}
            className={`${inputCls} mono`}
          />
          {helpEl}
        </div>
      );
  }
}

function Result({ data }: { data: { content?: any[]; structuredContent?: any; isError?: boolean } }) {
  return (
    <div>
      <div className="text-sm text-muted-foreground mb-1">
        Result {data.isError && <span className="text-destructive font-medium">(IsError)</span>}
      </div>
      {data.structuredContent && (
        <div className="mb-2">
          <div className="text-xs text-muted-foreground mb-1">structuredContent</div>
          <pre className="bg-card border border-border rounded p-2 mono text-xs overflow-auto">
{JSON.stringify(data.structuredContent, null, 2)}
          </pre>
        </div>
      )}
      {data.content && data.content.length > 0 && (
        <div>
          <div className="text-xs text-muted-foreground mb-1">
            content ({data.content.length} {data.content.length === 1 ? "block" : "blocks"})
          </div>
          <div className="space-y-2">
            {data.content.map((c: any, i: number) => (
              <pre key={i} className="bg-card border border-border rounded p-2 mono text-xs overflow-auto whitespace-pre-wrap">
{c.type === "text" ? c.text : JSON.stringify(c, null, 2)}
              </pre>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

const inputCls =
  "w-full bg-background border border-input rounded px-3 py-1.5 text-sm focus:outline-none focus:ring-2 focus:ring-ring";

// fieldsFromSchema produces a reasonable Field[] from a JSON Schema object.
// Returns null when the schema isn't usable (missing, not an object, or no
// properties), in which case the caller falls back to a "Run with empty
// args" button. Honors: type (string/integer/number/boolean), enum,
// description, default, minimum, maximum.
function fieldsFromSchema(schema: unknown): Field[] | null {
  if (!schema || typeof schema !== "object") return null;
  const s = schema as { type?: string; properties?: Record<string, JSONSchemaProp>; required?: string[] };
  if (s.type !== "object" || !s.properties) return null;
  const required = new Set(s.required ?? []);
  const out: Field[] = [];
  for (const [name, prop] of Object.entries(s.properties)) {
    const f = fieldFromProp(name, prop, required.has(name));
    if (f) out.push(f);
  }
  return out;
}

type JSONSchemaProp = {
  type?: string | string[];
  description?: string;
  default?: unknown;
  enum?: unknown[];
  minimum?: number;
  maximum?: number;
  title?: string;
};

function fieldFromProp(name: string, prop: JSONSchemaProp, required: boolean): Field | null {
  const label = prop.title ?? name;
  const help = prop.description;
  const t = Array.isArray(prop.type) ? prop.type[0] : prop.type;

  if (Array.isArray(prop.enum) && prop.enum.length > 0) {
    return {
      kind: "select",
      name,
      label,
      help,
      default: typeof prop.default === "string" ? prop.default : undefined,
      options: prop.enum.map((v) => ({ value: String(v), label: String(v) })),
    };
  }
  switch (t) {
    case "boolean":
      return { kind: "boolean", name, label, help, default: typeof prop.default === "boolean" ? prop.default : false };
    case "integer":
    case "number":
      return {
        kind: "number",
        name,
        label,
        help,
        default: typeof prop.default === "number" ? prop.default : undefined,
        min: prop.minimum,
        max: prop.maximum,
        required,
      };
    case "object":
    case "array":
      return {
        kind: "json",
        name,
        label,
        help: (help ?? "") + " (JSON)",
        default: prop.default !== undefined ? JSON.stringify(prop.default, null, 2) : "",
      };
    case "string":
    default:
      return {
        kind: "text",
        name,
        label,
        help,
        default: typeof prop.default === "string" ? prop.default : undefined,
        required,
      };
  }
}

function initialValues(fields: Field[] | null): Record<string, any> {
  const out: Record<string, any> = {};
  if (!fields) return out;
  for (const f of fields) {
    switch (f.kind) {
      case "text":    out[f.name] = f.default ?? ""; break;
      case "number":  out[f.name] = f.default ?? ""; break;
      case "slider":  out[f.name] = f.default; break;
      case "boolean": out[f.name] = f.default ?? false; break;
      case "select":  out[f.name] = f.default ?? f.options[0]?.value ?? ""; break;
      case "json":    out[f.name] = f.default ?? ""; break;
    }
  }
  return out;
}

// buildArgs converts the form state into the JSON-RPC arguments object the
// MCP tool expects. Empty optional strings/numbers are dropped. JSON fields
// are parsed; if parsing fails, the field is sent as a string and the server
// will reject it.
//
// Booleans are sent unconditionally: an unchecked checkbox must surface as
// `false`, not be omitted (otherwise the server falls back to its default
// and the user sees behavior that contradicts the form).
function buildArgs(fields: Field[] | null, values: Record<string, any>): Record<string, any> {
  const out: Record<string, any> = {};
  if (!fields) return out;
  for (const f of fields) {
    const v = values[f.name];
    switch (f.kind) {
      case "text":
        if (v !== "" && v != null) out[f.name] = v;
        break;
      case "number":
      case "slider":
        if (v !== "" && v != null && !Number.isNaN(Number(v))) out[f.name] = Number(v);
        break;
      case "boolean":
        out[f.name] = Boolean(v);
        break;
      case "select":
        if (v) out[f.name] = v;
        break;
      case "json":
        if (v && String(v).trim()) {
          try { out[f.name] = JSON.parse(v); } catch { /* skip on parse error */ }
        }
        break;
    }
  }
  return out;
}
