package handler

import (
	"context"
	"encoding/json"
	"errors"
	"forge/internal/httpresponse"
	"forge/internal/token"
	"forge/internal/worker"
	"io"
	"log"
	"mime"
	"net/http"
)

type WorkerHandler struct {
	service WorkerService
}

type WorkerService interface {
	Register(ctx context.Context, bootstrapToken string, input worker.RegisterWorkerInput) (worker.RegisterWorkerOutput, error)
}

func (h *WorkerHandler) Register(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))

	if err != nil {
		httpresponse.ResponseError(w, http.StatusBadRequest, httpresponse.ErrCodeMediaTypeParseError, "Failed to parse media type")
		return
	}

	if mediaType != "application/json" {
		httpresponse.ResponseError(w, http.StatusUnsupportedMediaType, httpresponse.ErrCodeMediaTypeError, "JSON content-type required")
		return
	}

	bootstrapToken, err := token.ExtractBearerToken(r)
	if err != nil {
		httpresponse.ResponseError(w, http.StatusUnauthorized, httpresponse.ErrCodeNotAuthorized, "Not authorized")
		return
	}

	var input worker.RegisterWorkerInput
	decoder := json.NewDecoder(r.Body)

	if err := decoder.Decode(&input); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			httpresponse.ResponseError(w, http.StatusRequestEntityTooLarge, httpresponse.ErrCodeRequestBodyLimit, "Request body too large. Limit is 1MB")
			return
		}

		httpresponse.ResponseError(w, http.StatusBadRequest, httpresponse.ErrCodeInvalidJSONPayload, "Invalid JSON payload")
		return
	}

	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		httpresponse.ResponseError(w, http.StatusBadRequest, httpresponse.ErrCodeInvalidJSONPayload, "Invalid JSON payload: expected a single JSON value")
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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(output); err != nil {
		log.Printf("failed to encode registered worker: %v", err)
	}
}
