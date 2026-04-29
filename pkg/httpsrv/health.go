package httpsrv

import (
	"net/http"
	"sync/atomic"
)

// Readiness tracks the server's "ready to accept new traffic" flag. Flipped to
// false during shutdown so load balancers can drain.
type Readiness struct {
	ready atomic.Bool
}

// NewReadiness returns a Readiness initialised to true.
func NewReadiness() *Readiness {
	r := &Readiness{}
	r.ready.Store(true)
	return r
}

// SetReady toggles the flag.
func (r *Readiness) SetReady(v bool) { r.ready.Store(v) }

// HealthzHandler returns 200 unconditionally; used for liveness.
func HealthzHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}
}

// ReadyzHandler returns 200 if ready, 503 otherwise.
func (r *Readiness) ReadyzHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if r.ready.Load() {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ready"))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("draining"))
	}
}
