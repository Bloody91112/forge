package handler

import (
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
	service *task.Service
}

func NewTaskHandler(service *task.Service) *TaskHandler {
	return &TaskHandler{service: service}
}

func (h *TaskHandler) CreateTask(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))

	if err != nil {
		http.Error(w, "Failed to parse media type", http.StatusBadRequest)
		return
	}

	if mediaType != "application/json" {
		http.Error(w, "json required", http.StatusBadRequest)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var taskInput task.CreateTaskInput
	decoder := json.NewDecoder(r.Body)

	if err := decoder.Decode(&taskInput); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			http.Error(w, "Request body too large. Limit is 1MB.", http.StatusRequestEntityTooLarge)
			return
		}

		http.Error(w, "invalid json payload", http.StatusBadRequest)
		return
	}

	// check and reject additional json objects
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		http.Error(w, "invalid json payload", http.StatusBadRequest)
		return
	}

	createdTask, err := h.service.Create(r.Context(), taskInput)

	if errors.Is(err, task.ErrInvalidInput) {
		http.Error(w, "invalid payload params", http.StatusBadRequest)
		return
	}

	if errors.Is(err, task.ErrMissingClientID) {
		log.Printf("no client id provided, err: %v", err)
		http.Error(w, "failed to create task", http.StatusInternalServerError)
		return
	}

	if errors.Is(err, task.ErrIdempotencyConflict) {
		http.Error(w, "idempotency key conflict", http.StatusConflict)
		return
	}

	if err != nil {
		log.Printf("create task unknown err: %v", err)
		http.Error(w, "failed to create task", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)

	if err := json.NewEncoder(w).Encode(createdTask); err != nil {
		log.Printf("failed to encode created task: %v", err)
	}
}

func (h *TaskHandler) GetTask(w http.ResponseWriter, r *http.Request) {

	id, err := uuid.Parse(r.PathValue("id"))

	if err != nil {
		http.Error(w, "invalid task id", http.StatusBadRequest)
		return
	}

	gotTask, err := h.service.GetByID(r.Context(), id)

	if errors.Is(err, task.ErrTaskNotFound) {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}

	if err != nil {
		log.Printf("failed to get task: %v", err)
		http.Error(w, "failed to get task", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(gotTask); err != nil {
		log.Printf("failed to encode task: %v", err)
		http.Error(w, "failed to get task", http.StatusInternalServerError)
		return
	}
}
