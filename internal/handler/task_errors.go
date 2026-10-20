package handler

const (
	errCodeMediaTypeParseError = "media_type_parse_error"
	errCodeMediaTypeError      = "media_type_error"

	errCodeIdempotencyKeyError = "idempotency_key_error"
	errCodeIdempotencyConflict = "idempotency_key_conflict"

	errCodeInvalidJSONPayload = "invalid_json_payload"
	errCodeRequestBodyLimit   = "request_body_limit_error"
	errCodeInvalidPayload     = "invalid_payload_params"

	errCodeCreateTaskFailed = "create_task_error"

	errCodeInvalidTaskID = "invalid_task_id"
	errCodeTaskNotFound  = "task_not_found"
	errCodeGetTaskFailed = "get_task_failed"
)
