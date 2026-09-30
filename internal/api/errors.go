package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
)

// maxBodyBytes bounds request bodies; chapters are text, so 16 MB is generous.
const maxBodyBytes = 16 << 20

// httpError is an error with a status and a stable code for the frontend.
type httpError struct {
	status int
	code   string
	msg    string
}

func (e *httpError) Error() string { return e.msg }

func errBadRequest(format string, a ...any) error {
	return &httpError{status: http.StatusBadRequest, code: "bad_request", msg: fmt.Sprintf(format, a...)}
}

func errNotFound(what string) error {
	return &httpError{status: http.StatusNotFound, code: "not_found", msg: what + " not found"}
}

func errConflict(msg string) error {
	return &httpError{status: http.StatusConflict, code: "conflict", msg: msg}
}

func errGateway(msg string) error {
	return &httpError{status: http.StatusBadGateway, code: "gateway_error", msg: msg}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	body := ErrorResponse{}
	body.Error.Code = code
	body.Error.Message = msg
	writeJSON(w, status, body)
}

// fail maps an error to a JSON error reply.
func (s *Server) fail(w http.ResponseWriter, err error) {
	var he *httpError
	switch {
	case errors.As(err, &he):
		writeError(w, he.status, he.code, he.msg)
	case errors.Is(err, pgx.ErrNoRows):
		writeError(w, http.StatusNotFound, "not_found", "not found")
	default:
		if s.log != nil {
			s.log.Error("request failed", "err", err)
		} else {
			slog.Error("request failed", "err", err)
		}
		writeError(w, http.StatusInternalServerError, "internal", "something went wrong on the server")
	}
}

func decodeJSON(r *http.Request, v any) error {
	body := http.MaxBytesReader(nil, r.Body, maxBodyBytes)
	dec := json.NewDecoder(body)
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return errBadRequest("request body is empty")
		}
		return errBadRequest("invalid JSON: %v", err)
	}
	return nil
}
