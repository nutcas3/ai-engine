package errs

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStatusThroughWrap(t *testing.T) {
	inner := E("store.enqueue", Internal, errors.New("conn refused"))
	mid := fmt.Errorf("clusters.create: %w", inner)
	if got := Status(mid); got != http.StatusInternalServerError {
		t.Fatalf("got %d", got)
	}
	nf := fmt.Errorf("nodes.drain: %w", ErrNotFound)
	if got := Status(nf); got != http.StatusNotFound {
		t.Fatalf("got %d", got)
	}
	if got := Status(E("capi.provision", Provider, ErrNotConfigured)); got != http.StatusBadGateway {
		t.Fatalf("got %d", got)
	}
}

func TestWriteEmitsCleanJSON(t *testing.T) {
	w := httptest.NewRecorder()
	Write(w, E("clusters.delete", NotFound, ErrNotFound, "cluster not found"))
	if w.Code != http.StatusNotFound {
		t.Fatalf("code %d", w.Code)
	}
	body := w.Body.String()
	if body == "" || !contains(body, "not_found") || contains(body, "ErrNotFound") {
		t.Fatalf("bad body: %s", body)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
