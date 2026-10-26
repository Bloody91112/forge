package handler

import (
	"context"
	"errors"
	"forge/internal/httpresponse"
	"forge/internal/token"
	"forge/internal/worker"
	"log"
	"net/http"
)

type WorkerHandler struct {
	baseHandler BaseHandler
	service     WorkerService
}

func NewWorkerHandler(service WorkerService) *WorkerHandler {
	return &WorkerHandler{
		baseHandler: BaseHandler{},
		service:     service,
	}
}

type WorkerService interface {
	Register(ctx context.Context, bootstrapToken string, input worker.RegisterWorkerInput) (worker.RegisterWorkerOutput, error)
}

func (h *WorkerHandler) Register(w http.ResponseWriter, r *http.Request) {
	if !h.baseHandler.CheckJSONContentType(w, r) {
		return
	}

	bootstrapToken, err := token.ExtractBearerToken(r)
	if err != nil {
		httpresponse.ResponseError(w, http.StatusUnauthorized, httpresponse.ErrCodeNotAuthorized, "Not authorized")
		return
	}

	var input worker.RegisterWorkerInput
	if !h.baseHandler.DecodeJSONAndCheckErrors(w, r, input, 1<<20) {
		return
	}

	output, err := h.service.Register(r.Context(), bootstrapToken, input)

	if errors.Is(err, worker.ErrInvalidInput) {
		httpresponse.ResponseError(w, http.StatusBadRequest, httpresponse.ErrCodeInvalidPayload, "Invalid payload params")
		return
	}

	if errors.Is(err, worker.ErrInvalidBootstrapToken) {
		httpresponse.ResponseError(w, http.StatusUnauthorized, httpresponse.ErrCodeAuthFailed, "Not authorized")
		return
	}

	if errors.Is(err, worker.ErrUnsupportedProtocolVersion) {
		httpresponse.ResponseError(w, http.StatusUnprocessableEntity, httpresponse.ErrUnsupportedProtocolVersion, "Unsupported protocol version")
		return
	}

	if err != nil {
		log.Printf("register worker unknown err: %v", err)
		httpresponse.ResponseError(w, http.StatusInternalServerError, httpresponse.ErrCodeRegisterWorkerFailed, "Failed to register worker")
		return
	}

	if err := h.baseHandler.EncodeJSONResponse(w, http.StatusOK, "registered worker", output); err != nil {
		log.Print(err)
	}
}

func (h *WorkerHandler) Heartbeat(w http.ResponseWriter, r *http.Request) {}
