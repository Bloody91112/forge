package handler

import (
	"context"
	"net/http"
	"sync/atomic"
	"time"
)

type Pingler interface {
	Ping(context context.Context) error
}

type ReadinessHandler struct {
	db           Pingler
	shuttingDown atomic.Bool
}

func NewReadinessHandler(db Pingler) *ReadinessHandler {
	return &ReadinessHandler{db: db}
}

func (h *ReadinessHandler) Check(w http.ResponseWriter, r *http.Request) {
	if h.shuttingDown.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := h.db.Ping(ctx); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *ReadinessHandler) SetNotReady() {
	h.shuttingDown.Swap(true)
}
