package handler

import "net/http"

type HealthHandler struct{}

func (h *HealthHandler) Check(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}
