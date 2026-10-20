package handler

import (
	"context"
	"encoding/json"
	"errors"
	"forge/internal/task"
	"io"
	"log"
	"mime"
	"net/http"

	"github.com/google/uuid"
)

type TaskHandler struct {
	service TaskService
}

type TaskService interface {
	Create(ctx context.Context, dto task.CreateTaskInput) (task.Task, error)
	GetByID(ctx context.Context, id uuid.UUID) (task.Task, error)
}

func NewTaskHandler(service TaskService) *TaskHandler {
	return &TaskHandler{service: service}
}

func (h *TaskHandler) CreateTask(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))

	if err != nil {
		responseError(w, http.StatusBadRequest, errCodeMediaTypeParseError, "Failed to parse media type")
		return
	}

	if mediaType != "application/json" {
		responseError(w, http.StatusBadRequest, errCodeMediaTypeError, "JSON content-type required")
		return
	}

	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		responseError(w, http.StatusBadRequest, errCodeIdempotencyKeyError, "Idempotency-Key header not provided")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var taskInput task.CreateTaskInput
	decoder := json.NewDecoder(r.Body)

	if err := decoder.Decode(&taskInput); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			responseError(w, http.StatusRequestEntityTooLarge, errCodeRequestBodyLimit, "Request body too large. Limit is 1MB")
			return
		}

		responseError(w, http.StatusBadRequest, errCodeInvalidJSONPayload, "Invalid JSON payload")
		return
	}

	// check and reject additional json objects
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		responseError(w, http.StatusBadRequest, errCodeInvalidJSONPayload, "Invalid JSON payload: expected a single JSON value")
		return
	}

	taskInput.IdempotencyKey = idempotencyKey
	createdTask, err := h.service.Create(r.Context(), taskInput)

	if errors.Is(err, task.ErrInvalidInput) {
		responseError(w, http.StatusBadRequest, errCodeInvalidPayload, "Invalid payload params")
		return
	}

	if errors.Is(err, task.ErrMissingClientID) {
		log.Printf("no client id provided, err: %v", err)
		responseError(w, http.StatusInternalServerError, errCodeCreateTaskFailed, "Failed to create task")
		return
	}

	if errors.Is(err, task.ErrIdempotencyConflict) {
		responseError(w, http.StatusConflict, errCodeIdempotencyConflict, "Idempotency key conflict")
		return
	}

	if err != nil {
		log.Printf("create task unknown err: %v", err)
		responseError(w, http.StatusInternalServerError, errCodeCreateTaskFailed, "Failed to create task")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)

	if err := json.NewEncoder(w).Encode(newCreateTaskResponse(createdTask)); err != nil {
		log.Printf("failed to encode created task: %v", err)
	}
}

func (h *TaskHandler) GetTask(w http.ResponseWriter, r *http.Request) {

	id, err := uuid.Parse(r.PathValue("id"))

	if err != nil {
		responseError(w, http.StatusBadRequest, errCodeInvalidTaskID, "Invalid task id")
		return
	}

	gotTask, err := h.service.GetByID(r.Context(), id)

	if errors.Is(err, task.ErrTaskNotFound) {
		responseError(w, http.StatusNotFound, errCodeTaskNotFound, "Task not found")
		return
	}

	if err != nil {
		log.Printf("failed to get task: %v", err)
		responseError(w, http.StatusInternalServerError, errCodeGetTaskFailed, "Get task failed")
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(newGetTaskResponse(gotTask)); err != nil {
		log.Printf("failed to encode task: %v", err)
		return
	}
}

func responseError(w http.ResponseWriter, status int, errCode, errMessage string) {
	errObject := ErrorResponse{
		Error: struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}{Code: errCode, Message: errMessage},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(errObject); err != nil {
		log.Printf("failed to encode error response: %v", err)
	}
}
