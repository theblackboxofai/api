package models

import "net/http"

type StatsHandler struct {
	service *Service
}

func NewStatsHandler(service *Service) *StatsHandler {
	return &StatsHandler{service: service}
}

func (h *StatsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}

	response, err := h.service.ListStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_server_error", "failed to list stats")
		return
	}

	writeJSON(w, http.StatusOK, response)
}
