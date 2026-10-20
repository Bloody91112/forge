package httpresponse

import (
	"encoding/json"
	"log"
	"net/http"
)

const (
	ErrCodeMediaTypeParseError = "media_type_parse_error"
	ErrCodeMediaTypeError      = "media_type_error"
	ErrCodeIdempotencyKeyError = "idempotency_key_error"
	ErrCodeIdempotencyConflict = "idempotency_key_conflict"
	ErrCodeInvalidJSONPayload  = "invalid_json_payload"
	ErrCodeRequestBodyLimit    = "request_body_limit_error"
	ErrCodeInvalidPayload      = "invalid_payload_params"
	ErrCodeCreateTaskFailed    = "create_task_error"
	ErrCodeInvalidTaskID       = "invalid_task_id"
	ErrCodeTaskNotFound        = "task_not_found"
	ErrCodeGetTaskFailed       = "get_task_failed"
	ErrCodeNotAuthorized       = "not_authorized"
	ErrCodeAuthFailed          = "failed_to_authorize"
)

type ErrorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func ResponseError(w http.ResponseWriter, status int, errCode, errMessage string) {
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
