package handler

import (
	"context"
	"net/http"
	"sync/atomic"
	"time"
)

type PingableDb interface {
	Ping(context context.Context) error
}

type ReadinessHandler struct {
	DB           PingableDb
	shuttingDown atomic.Bool
}

func NewReadinessHandler(DB PingableDb) *ReadinessHandler {
	return &ReadinessHandler{DB: DB}
}

func (h *ReadinessHandler) Check(w http.ResponseWriter, r *http.Request) {
	if h.shuttingDown.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := h.DB.Ping(ctx); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *ReadinessHandler) SetNotReady() {
	h.shuttingDown.Swap(true)
}
