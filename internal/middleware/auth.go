package middleware

import (
	"errors"
	"forge/internal/auth"
	"forge/internal/httpresponse"
	"forge/internal/task"
	"forge/internal/token"
	"log"
	"net/http"
)

func Auth(authService *auth.Service, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientID, err := authService.VerifyClientByToken(r)
		if err != nil {
			switch {
			case errors.Is(err, token.ErrInvalidAuthToken),
				errors.Is(err, auth.ErrClientNotFound):
				httpresponse.ResponseError(w, http.StatusUnauthorized, httpresponse.ErrCodeNotAuthorized, "Not authorized")

			default:
				log.Printf("authentication failed: %v", err)
				httpresponse.ResponseError(w, http.StatusInternalServerError, httpresponse.ErrCodeAuthFailed, "Failed to authorize")
			}

			return
		}

		ctx := task.WithClientID(r.Context(), clientID)
		r = r.WithContext(ctx)

		next.ServeHTTP(w, r)
	})
}
