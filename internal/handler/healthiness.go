package handler

import "net/http"

type HealthinessHandler struct{}

func (h *HealthinessHandler) Check(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}
