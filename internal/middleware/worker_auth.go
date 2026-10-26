package middleware

import (
	"errors"
	"forge/internal/auth"
	"forge/internal/httpresponse"
	"forge/internal/token"
	"log"
	"net/http"
)

func WorkerAuth(authService *auth.WorkerService, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := authService.VerifyWorkerByToken(r); err != nil {
			switch {
			case errors.Is(err, token.ErrInvalidAuthToken),
				errors.Is(err, auth.ErrWorkerNotFound):
				httpresponse.ResponseError(w, http.StatusUnauthorized, httpresponse.ErrCodeNotAuthorized, "Not authorized")

			default:
				log.Printf("worker authentication failed: %v", err)
				httpresponse.ResponseError(w, http.StatusInternalServerError, httpresponse.ErrCodeAuthFailed, "Failed to authorize")
			}

			return
		}

		next.ServeHTTP(w, r)
	})
}
