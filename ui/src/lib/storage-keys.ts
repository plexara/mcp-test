// Centralized localStorage key names so unrelated modules
// (the Audit page that writes the stash, the auth store that clears it
// on sign-out) reference the same constant.
export const COMPARE_KEY = "audit-compare-stash";
