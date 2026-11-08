// Package errs is the structured error layer: every package returns sentinel
// errors for decisions and wraps with E(op, kind, err) so context stays
// intact without verbose stacking. The presentation layer translates Kind
// into HTTP status + a small JSON body — inner details stay in logs.
package errs

import (
	"encoding/json"
	"errors"
	"net/http"
)

type Kind string

const (
	Invalid  Kind = "invalid"   // 400 — client payload
	NotFound Kind = "not_found" // 404
	Conflict Kind = "conflict"  // 409
	Provider Kind = "provider"  // 502 — upstream/CAPI failure
	Internal Kind = "internal"  // 500
)

// Error carries the operation, the classification, and the cause. Wrap chains
// stay inspectable via errors.Is/As; Msg is what clients see.
type Error struct {
	Op   string `json:"op"`   // e.g. "provision.cluster"
	Kind Kind   `json:"kind"`
	Msg  string `json:"message"`
	Err  error  `json:"-"`
}

func (e *Error) Error() string {
	if e.Err != nil {
		return e.Op + ": " + e.Msg + ": " + e.Err.Error()
	}
	return e.Op + ": " + e.Msg
}

func (e *Error) Unwrap() error { return e.Err }

// E wraps err with layer context. If err is already classified, the outer Op
// is recorded but the innermost Kind wins for status mapping.
func E(op string, k Kind, err error, msg ...string) *Error {
	m := http.StatusText(0)
	if len(msg) > 0 {
		m = msg[0]
	} else {
		m = string(k)
	}
	return &Error{Op: op, Kind: k, Msg: m, Err: err}
}

// Sentinels callers can errors.Is against — one per semantic failure, reused
// across layers instead of string-matching.
var (
	ErrNotFound     = &Error{Kind: NotFound, Msg: "not found"}
	ErrNotConfigured = &Error{Kind: Provider, Msg: "provider not configured"}
	ErrConflict     = &Error{Kind: Conflict, Msg: "conflict"}
	ErrInvalid      = &Error{Kind: Invalid, Msg: "invalid request"}
)

// Status maps an error chain to an HTTP status: explicit *Error Kind wins,
// then sentinels, then 500.
func Status(err error) int {
	if err == nil {
		return http.StatusOK
	}
	var ae *Error
	if errors.As(err, &ae) && ae.Kind != "" {
		switch ae.Kind {
		case Invalid:
			return http.StatusBadRequest
		case NotFound:
			return http.StatusNotFound
		case Conflict:
			return http.StatusConflict
		case Provider:
			return http.StatusBadGateway
		}
	}
	switch {
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrNotConfigured):
		return http.StatusServiceUnavailable
	case errors.Is(err, ErrConflict):
		return http.StatusConflict
	case errors.Is(err, ErrInvalid):
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

// Write renders the error as {error:{kind,message}}. The Op and cause are
// logged server-side; clients get the clean classification.
func Write(w http.ResponseWriter, err error) {
	code := Status(err)
	var ae *Error
	msg := http.StatusText(code)
	if errors.As(err, &ae) && ae.Msg != "" {
		msg = ae.Msg
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{"kind": kindOf(err, code), "message": msg},
	})
}

func kindOf(err error, code int) string {
	var ae *Error
	if errors.As(err, &ae) && ae.Kind != "" {
		return string(ae.Kind)
	}
	switch code {
	case http.StatusNotFound:
		return string(NotFound)
	case http.StatusBadRequest:
		return string(Invalid)
	case http.StatusConflict:
		return string(Conflict)
	case http.StatusBadGateway, http.StatusServiceUnavailable:
		return string(Provider)
	}
	return string(Internal)
}
