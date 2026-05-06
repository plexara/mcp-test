import { create } from "zustand";
import { portalAPI, clearApiKey, setUnauthorizedHandler, type Identity } from "@/lib/api";
import { COMPARE_KEY } from "@/lib/storage-keys";

// clearSessionScopedState wipes any portal state that should not survive
// a sign-out on a shared workstation: the audit-compare stash currently,
// any future per-user UI state added here. API keys and zustand state
// are reset by the caller; this is the localStorage piece.
function clearSessionScopedState() {
  try {
    localStorage.removeItem(COMPARE_KEY);
  } catch {
    /* localStorage unavailable (incognito quota / disabled) — ignore */
  }
}

type Status = "idle" | "loading" | "authenticated" | "anonymous";

type AuthState = {
  identity: Identity | null;
  status: Status;
  refresh: () => Promise<void>;
  signOut: () => Promise<void>;
};

export const useAuth = create<AuthState>((set) => ({
  identity: null,
  status: "idle",
  refresh: async () => {
    set({ status: "loading" });
    try {
      const id = await portalAPI.me();
      set({ identity: id, status: "authenticated" });
    } catch {
      set({ identity: null, status: "anonymous" });
    }
  },
  signOut: async () => {
    clearApiKey();
    clearSessionScopedState();
    try {
      // CSRF: the request() wrapper sends X-Requested-With automatically
      // when called via api.post; here we hit the auth endpoint directly
      // because it's not under /api/v1/*.
      await fetch("/portal/auth/logout", {
        method: "POST",
        credentials: "include",
        headers: { "X-Requested-With": "XMLHttpRequest" },
      });
    } catch { /* ignore */ }
    set({ identity: null, status: "anonymous" });
    window.location.href = "/portal/login";
  },
}));

// Wire the API client's 401 hook on import so any in-flight request
// hitting an expired session immediately drops the SPA back to login.
setUnauthorizedHandler(() => {
  clearApiKey();
  clearSessionScopedState();
  useAuth.setState({ identity: null, status: "anonymous" });
  if (typeof window !== "undefined" && !window.location.pathname.endsWith("/login")) {
    window.location.href = "/portal/login";
  }
});
