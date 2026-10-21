package handler

import (
	"context"
	"encoding/json"
	"errors"
	"forge/internal/httpresponse"
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
	GetByID(ctx context.Context, taskID uuid.UUID) (task.Task, error)
}

func NewTaskHandler(service TaskService) *TaskHandler {
	return &TaskHandler{service: service}
}

func (h *TaskHandler) CreateTask(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))

	if err != nil {
		httpresponse.ResponseError(w, http.StatusBadRequest, httpresponse.ErrCodeMediaTypeParseError, "Failed to parse media type")
		return
	}

	if mediaType != "application/json" {
		httpresponse.ResponseError(w, http.StatusBadRequest, httpresponse.ErrCodeMediaTypeError, "JSON content-type required")
		return
	}

	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		httpresponse.ResponseError(w, http.StatusBadRequest, httpresponse.ErrCodeIdempotencyKeyError, "Idempotency-Key header not provided")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var taskInput task.CreateTaskInput
	decoder := json.NewDecoder(r.Body)

	if err := decoder.Decode(&taskInput); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			httpresponse.ResponseError(w, http.StatusRequestEntityTooLarge, httpresponse.ErrCodeRequestBodyLimit, "Request body too large. Limit is 1MB")
			return
		}

		httpresponse.ResponseError(w, http.StatusBadRequest, httpresponse.ErrCodeInvalidJSONPayload, "Invalid JSON payload")
		return
	}

	// check and reject additional json objects
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		httpresponse.ResponseError(w, http.StatusBadRequest, httpresponse.ErrCodeInvalidJSONPayload, "Invalid JSON payload: expected a single JSON value")
		return
	}

	taskInput.IdempotencyKey = idempotencyKey
	createdTask, err := h.service.Create(r.Context(), taskInput)

	if errors.Is(err, task.ErrInvalidInput) {
		httpresponse.ResponseError(w, http.StatusBadRequest, httpresponse.ErrCodeInvalidPayload, "Invalid payload params")
		return
	}

	if errors.Is(err, task.ErrMissingClientID) {
		log.Printf("no client id provided, err: %v", err)
		httpresponse.ResponseError(w, http.StatusInternalServerError, httpresponse.ErrCodeCreateTaskFailed, "Failed to create task")
		return
	}

	if errors.Is(err, task.ErrIdempotencyConflict) {
		httpresponse.ResponseError(w, http.StatusConflict, httpresponse.ErrCodeIdempotencyConflict, "Idempotency key conflict")
		return
	}

	if err != nil {
		log.Printf("create task unknown err: %v", err)
		httpresponse.ResponseError(w, http.StatusInternalServerError, httpresponse.ErrCodeCreateTaskFailed, "Failed to create task")
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
		httpresponse.ResponseError(w, http.StatusBadRequest, httpresponse.ErrCodeInvalidTaskID, "Invalid task id")
		return
	}

	gotTask, err := h.service.GetByID(r.Context(), id)

	if errors.Is(err, task.ErrTaskNotFound) {
		httpresponse.ResponseError(w, http.StatusNotFound, httpresponse.ErrCodeTaskNotFound, "Task not found")
		return
	}

	if err != nil {
		log.Printf("failed to get task: %v", err)
		httpresponse.ResponseError(w, http.StatusInternalServerError, httpresponse.ErrCodeGetTaskFailed, "Get task failed")
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(newGetTaskResponse(gotTask)); err != nil {
		log.Printf("failed to encode task: %v", err)
		return
	}
}
