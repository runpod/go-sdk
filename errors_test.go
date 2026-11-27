package runpod

import (
	"errors"
	"net/http"
	"testing"
)

func TestCheckResponse(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   string // "" for no error
	}{
		{"ok", 200, `{}`, ""},
		{"accepted", 202, `{}`, ""},
		{"problem", 404, `{"title":"Not Found","status":404,"detail":"pod not found"}`, "runpod: HTTP 404 Not Found"},
		{"validation", 422, `{"title":"Unprocessable Entity","status":422,"detail":"Request validation failed.","errors":["$.name: required","$.gpu: required"]}`,
			"runpod: HTTP 422 Unprocessable Entity"},
		{"not JSON", 502, `<html>bad gateway</html>`, "runpod: HTTP 502 Bad Gateway"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckResponse(&http.Response{StatusCode: tt.status}, []byte(tt.body))
			if tt.want == "" {
				if err != nil {
					t.Errorf("err = %v, want nil", err)
				}
				return
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("err = %v, want an *APIError", err)
			}
			if apiErr.StatusCode != tt.status || string(apiErr.Body) != tt.body {
				t.Errorf("StatusCode, Body = %d, %q; want %d, %q", apiErr.StatusCode, apiErr.Body, tt.status, tt.body)
			}
			if err.Error() != tt.want {
				t.Errorf("Error() = %q, want %q", err.Error(), tt.want)
			}
			if tt.status == 422 && (apiErr.Detail != "Request validation failed." || len(apiErr.Errors) != 2) {
				t.Errorf("Detail, Errors = %q, %q; want them decoded", apiErr.Detail, apiErr.Errors)
			}
		})
	}
}
