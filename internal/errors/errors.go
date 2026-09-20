package errors

import (
	"encoding/json"
	"net/http"
)

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

func Write(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(APIError{Code: code, Message: message})
}

// Удобные шорткаты.
func Unauthorized(w http.ResponseWriter, msg string) {
	Write(w, http.StatusUnauthorized, "unauthorized", msg)
}

func Forbidden(w http.ResponseWriter, msg string) {
	Write(w, http.StatusForbidden, "forbidden", msg)
}

func TooManyRequests(w http.ResponseWriter, retryAfter int) {
	w.Header().Set("Retry-After", itoa(retryAfter))
	Write(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
}

func BadGateway(w http.ResponseWriter, msg string) {
	Write(w, http.StatusBadGateway, "bad_gateway", msg)
}

func Internal(w http.ResponseWriter) {
	Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
}

func itoa(n int) string {
	if n <= 0 {
		return "1"
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
