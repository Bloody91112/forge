package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"forge/internal/httpresponse"
	"io"
	"mime"
	"net/http"
)

type BaseHandler struct{}

func (b *BaseHandler) CheckJSONContentType(w http.ResponseWriter, r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))

	if err != nil {
		httpresponse.ResponseError(w, http.StatusBadRequest, httpresponse.ErrCodeMediaTypeParseError, "Failed to parse media type")
		return false
	}

	if mediaType != "application/json" {
		httpresponse.ResponseError(w, http.StatusUnsupportedMediaType, httpresponse.ErrCodeMediaTypeError, "JSON content-type required")
		return false
	}

	return true
}

func (b *BaseHandler) DecodeJSONAndCheckErrors(w http.ResponseWriter, r *http.Request, target any, maxBytes int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	decoder := json.NewDecoder(r.Body)

	if err := decoder.Decode(target); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			maxBytesStr := fmt.Sprintf("%.2f MB", float64(maxBytes)/(1024*1024))
			httpresponse.ResponseError(w, http.StatusRequestEntityTooLarge, httpresponse.ErrCodeRequestBodyLimit, "Request body too large, max bytes: "+maxBytesStr)
			return false
		}

		httpresponse.ResponseError(w, http.StatusBadRequest, httpresponse.ErrCodeInvalidJSONPayload, "Invalid JSON payload")
		return false
	}

	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		httpresponse.ResponseError(w, http.StatusBadRequest, httpresponse.ErrCodeInvalidJSONPayload, "Invalid JSON payload: expected a single JSON value")
		return false
	}

	return true
}

func (b *BaseHandler) EncodeJSONResponse(w http.ResponseWriter, status int, targetName string, target any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(target); err != nil {
		return fmt.Errorf("failed to encode %s: %w", targetName, err)
	}

	return nil
}
