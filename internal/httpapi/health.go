package httpapi

import "net/http"

// healthz is liveness: the process is up and serving.
func (s *server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// readyz is readiness: the dependencies (the database) answer.
func (s *server) readyz(w http.ResponseWriter, r *http.Request) {
	if err := s.ready(r.Context()); err != nil {
		s.log.WarnContext(r.Context(), "readiness check failed", "error", err)
		writeProblem(w, r, http.StatusServiceUnavailable, "not_ready", "A dependency is not available.", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
