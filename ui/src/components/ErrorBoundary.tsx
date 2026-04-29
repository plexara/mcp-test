import { Component, type ReactNode } from "react";

type Props = { children: ReactNode };
type State = { error: Error | null };

// Top-level error boundary so a render-time exception in any page produces
// a recoverable surface instead of white-screening the entire portal.
// Wrap <Routes> in main.tsx with this; React Query already catches async
// errors inside individual queries.
export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: { componentStack?: string | null }) {
    // Best-effort; mainly here to surface the error in devtools rather than
    // swallow it. Wire to a real error tracker (Sentry, etc.) later.

    console.error("[portal] uncaught error", error, info.componentStack);
  }

  reset = () => {
    this.setState({ error: null });
    window.location.reload();
  };

  render() {
    if (this.state.error) {
      return (
        <div role="alert" style={{
          padding: "3rem 2rem",
          maxWidth: "640px",
          margin: "4rem auto",
          fontFamily: "system-ui, sans-serif",
        }}>
          <h1 style={{ fontSize: "1.5rem", fontWeight: 600 }}>Something went wrong</h1>
          <p style={{ marginTop: "1rem", opacity: 0.7 }}>
            The portal hit an unexpected error. Reloading usually clears it.
          </p>
          <pre style={{
            marginTop: "1rem",
            padding: "1rem",
            background: "rgba(127, 127, 127, 0.08)",
            borderRadius: "0.5rem",
            fontSize: "0.8rem",
            overflowX: "auto",
            whiteSpace: "pre-wrap",
          }}>
            {String(this.state.error?.message || this.state.error)}
          </pre>
          <button
            type="button"
            onClick={this.reset}
            style={{
              marginTop: "1.5rem",
              padding: "0.5rem 1rem",
              borderRadius: "0.375rem",
              border: "1px solid currentColor",
              background: "transparent",
              cursor: "pointer",
            }}
          >
            Reload
          </button>
        </div>
      );
    }
    return this.props.children;
  }
}
