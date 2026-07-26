package kafkaproducer

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerRejectsMissingValue(t *testing.T) {
	srv := Server{BootstrapServers: []string{"unused:9092"}}
	req := httptest.NewRequest(http.MethodPost, "/topics/test/messages", strings.NewReader(`{"key":"k"}`))
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "value is required") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestHandlerRejectsInvalidValueJSON(t *testing.T) {
	srv := Server{BootstrapServers: []string{"unused:9092"}}
	req := httptest.NewRequest(http.MethodPost, "/topics/test/messages", strings.NewReader(`{"value":{"broken"`))
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid request json") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestHealthz(t *testing.T) {
	srv := Server{}
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}
