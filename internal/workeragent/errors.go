package workeragent

import (
	"errors"
	"net/http"
	"net/url"

	"forge/internal/workeragent/forgeapi"
)

var errWorkerAuthorizationFailed = errors.New("worker authorization failed")

func isRetryableForgeError(err error) bool {
	if err == nil {
		return false
	}

	var remoteErr *forgeapi.RemoteError
	if errors.As(err, &remoteErr) {
		return remoteErr.StatusCode == http.StatusInternalServerError
	}

	var urlErr *url.Error
	return errors.As(err, &urlErr)
}

func isForgeAuthFailedError(err error) bool {
	var remoteErr *forgeapi.RemoteError
	return errors.As(err, &remoteErr) && remoteErr.StatusCode == http.StatusUnauthorized
}

func isWorkerAuthorizationFailedError(err error) bool {
	return errors.Is(err, errWorkerAuthorizationFailed)
}

func isTaskLeaseLostError(err error) bool {
	var remoteErr *forgeapi.RemoteError
	return errors.As(err, &remoteErr) &&
		remoteErr.StatusCode == http.StatusConflict &&
		remoteErr.Code == "lease_lost"
}
