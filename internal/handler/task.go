package handler

import (
	"context"
	"errors"
	"forge/internal/httpresponse"
	"forge/internal/task"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
)

const maxTaskRequestBodySize = 2 << 20

type TaskHandler struct {
	service     TaskService
	baseHandler BaseHandler
}

type TaskService interface {
	Create(ctx context.Context, dto task.CreateTaskInput) (task.CreateResult, error)
	GetByID(ctx context.Context, taskID uuid.UUID) (task.Task, error)
	Claim(ctx context.Context, dto task.ClaimTaskInput) (task.Task, error)
	Renew(ctx context.Context, dto task.RenewTaskInput) (time.Time, error)
	Complete(ctx context.Context, dto task.CompleteTaskInput) error
	Fail(ctx context.Context, dto task.FailTaskInput) error
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
	createResult, err := h.service.Create(r.Context(), taskInput)

	if errors.Is(err, task.ErrPayloadTooLarge) {
		httpresponse.ResponseError(w, http.StatusRequestEntityTooLarge, httpresponse.ErrCodePayloadTooLarge, "Payload exceeds maximum size of 1 MiB")
		return
	}

	if errors.Is(err, task.ErrInvalidRequestStructure) {
		httpresponse.ResponseError(w, http.StatusBadRequest, httpresponse.ErrCodeInvalidPayload, "Invalid request structure")
		return
	}

	if errors.Is(err, task.ErrInvalidInput) {
		httpresponse.ResponseError(w, http.StatusUnprocessableEntity, httpresponse.ErrCodeInvalidPayload, "Invalid payload params")
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

	if errors.Is(err, task.ErrNoActiveWorkers) {
		httpresponse.ResponseError(w, http.StatusServiceUnavailable, httpresponse.ErrCodeNoActiveWorkers, "No active workers at the moment")
		return
	}

	if errors.Is(err, task.ErrTaskTypeNotSupported) {
		httpresponse.ResponseError(w, http.StatusUnprocessableEntity, httpresponse.ErrCodeTaskTypeNotSupported, "Task type not supported by workers pool")
		return
	}

	if err != nil {
		log.Printf("create task unknown err: %v", err)
		httpresponse.ResponseError(w, http.StatusInternalServerError, httpresponse.ErrCodeCreateTaskFailed, "Failed to create task")
		return
	}

	status := http.StatusAccepted
	if !createResult.Created {
		status = http.StatusOK
	}

	if err := h.baseHandler.EncodeJSONResponse(w, status, "created task", newCreateTaskResponse(createResult.Task)); err != nil {
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

func (h *TaskHandler) ClaimTask(w http.ResponseWriter, r *http.Request) {
	if !h.baseHandler.CheckJSONContentType(w, r) {
		return
	}

	var input task.ClaimTaskInput

	if !h.baseHandler.DecodeJSONAndCheckErrors(w, r, &input, maxTaskRequestBodySize) {
		return
	}

	claimedTask, err := h.service.Claim(r.Context(), input)

	if errors.Is(err, task.ErrNoAvailableTasks) {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if errors.Is(err, task.ErrInvalidInput) {
		httpresponse.ResponseError(w, http.StatusBadRequest, httpresponse.ErrCodeInvalidPayload, "Invalid payload params")
		return
	}

	if err != nil {
		log.Printf("claim task unknown err: %v", err)
		httpresponse.ResponseError(w, http.StatusInternalServerError, httpresponse.ErrCodeFailedToClaimTask, "Failed to claim task")
		return
	}

	if err := h.baseHandler.EncodeJSONResponse(w, http.StatusOK, "claim task", newClaimTaskResponse(claimedTask)); err != nil {
		log.Print(err)
	}

}

func (h *TaskHandler) RenewTask(w http.ResponseWriter, r *http.Request) {
	id, ok := h.validateContentTypeAndGetTaskID(w, r)

	if !ok {
		return
	}

	var dto task.RenewTaskInput
	if !h.baseHandler.DecodeJSONAndCheckErrors(w, r, &dto, maxTaskRequestBodySize) {
		return
	}

	dto.TaskID = id

	leaseExpiresAt, err := h.service.Renew(r.Context(), dto)

	if errors.Is(err, task.ErrInvalidInput) {
		httpresponse.ResponseError(w, http.StatusUnprocessableEntity, httpresponse.ErrCodeInvalidPayload, "Invalid payload params")
		return
	}

	if errors.Is(err, task.ErrTaskLeaseLost) {
		httpresponse.ResponseError(w, http.StatusConflict, httpresponse.ErrCodeTaskLeaseLost, "Task lease lost")
		return
	}

	if err != nil {
		log.Printf("renew task unknown err: %v", err)
		httpresponse.ResponseError(w, http.StatusInternalServerError, httpresponse.ErrCodeFailedToRenewTask, "Failed to renew task")
		return
	}

	response := RenewTaskResponse{LeaseExpiresAt: leaseExpiresAt}

	if err := h.baseHandler.EncodeJSONResponse(w, http.StatusOK, "renew task", response); err != nil {
		log.Print(err)
	}
}

func (h *TaskHandler) CompleteTask(w http.ResponseWriter, r *http.Request) {
	id, ok := h.validateContentTypeAndGetTaskID(w, r)

	if !ok {
		return
	}

	var dto task.CompleteTaskInput
	if !h.baseHandler.DecodeJSONAndCheckErrors(w, r, &dto, maxTaskRequestBodySize) {
		return
	}

	dto.TaskID = id

	err := h.service.Complete(r.Context(), dto)

	if errors.Is(err, task.ErrTaskLeaseLost) {
		httpresponse.ResponseError(w, http.StatusConflict, httpresponse.ErrCodeTaskLeaseLost, "Task lease lost")
		return
	}

	if errors.Is(err, task.ErrPayloadTooLarge) {
		httpresponse.ResponseError(w, http.StatusRequestEntityTooLarge, httpresponse.ErrCodePayloadTooLarge, "Result exceeds maximum size of 1 MiB")
		return
	}

	if errors.Is(err, task.ErrInvalidRequestStructure) {
		httpresponse.ResponseError(w, http.StatusBadRequest, httpresponse.ErrCodeInvalidPayload, "Result must be a JSON object")
		return
	}

	if errors.Is(err, task.ErrInvalidInput) {
		httpresponse.ResponseError(w, http.StatusUnprocessableEntity, httpresponse.ErrCodeInvalidPayload, "Invalid payload params")
		return
	}

	if err != nil {
		log.Printf("complete task unknown err: %v", err)
		httpresponse.ResponseError(w, http.StatusInternalServerError, httpresponse.ErrCodeFailedToCompleteTask, "Failed to mark task as completed")
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *TaskHandler) FailTask(w http.ResponseWriter, r *http.Request) {
	id, ok := h.validateContentTypeAndGetTaskID(w, r)

	if !ok {
		return
	}

	var dto task.FailTaskInput
	if !h.baseHandler.DecodeJSONAndCheckErrors(w, r, &dto, maxTaskRequestBodySize) {
		return
	}

	dto.TaskID = id

	err := h.service.Fail(r.Context(), dto)

	if errors.Is(err, task.ErrTaskLeaseLost) {
		httpresponse.ResponseError(w, http.StatusConflict, httpresponse.ErrCodeTaskLeaseLost, "Task lease lost")
		return
	}

	if errors.Is(err, task.ErrInvalidInput) {
		httpresponse.ResponseError(w, http.StatusUnprocessableEntity, httpresponse.ErrCodeInvalidPayload, "Invalid payload params")
		return
	}

	if err != nil {
		log.Printf("fail task unknown err: %v", err)
		httpresponse.ResponseError(w, http.StatusInternalServerError, httpresponse.ErrCodeFailedToFailTask, "Failed to mark task as failed")
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *TaskHandler) validateContentTypeAndGetTaskID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	if !h.baseHandler.CheckJSONContentType(w, r) {
		return uuid.Nil, false
	}

	id, err := uuid.Parse(r.PathValue("id"))

	if err != nil {
		httpresponse.ResponseError(w, http.StatusBadRequest, httpresponse.ErrCodeInvalidTaskID, "Invalid task id")
		return uuid.Nil, false
	}

	return id, true
}
