package handler

import (
	"context"
	"errors"
	"forge/internal/httpresponse"
	"forge/internal/task"
	"log"
	"net/http"

	"github.com/google/uuid"
)

const maxTaskRequestBodySize = 2 << 20

type TaskHandler struct {
	service     TaskService
	baseHandler BaseHandler
}

type TaskService interface {
	Create(ctx context.Context, dto task.CreateTaskInput) (task.Task, error)
	GetByID(ctx context.Context, taskID uuid.UUID) (task.Task, error)
}

func NewTaskHandler(service TaskService) *TaskHandler {
	return &TaskHandler{
		service:     service,
		baseHandler: BaseHandler{},
	}
}

func (h *TaskHandler) CreateTask(w http.ResponseWriter, r *http.Request) {
	if !h.baseHandler.CheckJSONContentType(w, r) {
		return
	}

	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		httpresponse.ResponseError(w, http.StatusBadRequest, httpresponse.ErrCodeIdempotencyKeyError, "Idempotency-Key header not provided")
		return
	}

	var taskInput task.CreateTaskInput

	if !h.baseHandler.DecodeJSONAndCheckErrors(w, r, &taskInput, maxTaskRequestBodySize) {
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

	if err := h.baseHandler.EncodeJSONResponse(w, http.StatusAccepted, "created task", newCreateTaskResponse(createdTask)); err != nil {
		log.Print(err)
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

	if errors.Is(err, task.ErrMissingClientID) {
		log.Printf("no client id provided, err: %v", err)
		httpresponse.ResponseError(w, http.StatusInternalServerError, httpresponse.ErrCodeGetTaskFailed, "Failed to get task")
		return
	}

	if err != nil {
		log.Printf("failed to get task: %v", err)
		httpresponse.ResponseError(w, http.StatusInternalServerError, httpresponse.ErrCodeGetTaskFailed, "Get task failed")
		return
	}

	if err := h.baseHandler.EncodeJSONResponse(w, http.StatusOK, "got task", newGetTaskResponse(gotTask)); err != nil {
		log.Print(err)
	}
}
