package legacy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetReturnsObject(t *testing.T) {
	got := newCapture()

	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		got.set("method", r.Method)
		got.set("path", r.URL.Path)
		got.set("query", r.URL.RawQuery)
		got.set("auth", r.Header.Get("Authorization"))
		got.set("agent", r.Header.Get("User-Agent"))
		_, _ = io.WriteString(w, `{"id":"user-1","email":"a@b.c","gpuLimit":4}`)
	})

	user, err := testClient(t, srv.URL).Get(context.Background(), "user", map[string]string{"include": "quota usage"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.get("method") != http.MethodGet {
		t.Errorf("method = %s, want GET", got.get("method"))
	}
	if got.get("path") != "/user" {
		t.Errorf("path = %q, want /user", got.get("path"))
	}
	if got.get("query") != "include=quota+usage" {
		t.Errorf("query = %q, want the space percent-encoded", got.get("query"))
	}
	if got.get("auth") != "Bearer test-key" {
		t.Errorf("Authorization = %q", got.get("auth"))
	}
	if got.get("agent") != DefaultUserAgent {
		t.Errorf("User-Agent = %q, want %q", got.get("agent"), DefaultUserAgent)
	}
	if user["id"] != "user-1" || user["email"] != "a@b.c" || user["gpuLimit"].(float64) != 4 {
		t.Errorf("user = %v", user)
	}
}

func TestGetUnwrapsDataEnvelope(t *testing.T) {
	srv := testServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":{"id":"user-1"},"meta":{"requestId":"r-1"}}`)
	})

	user, err := testClient(t, srv.URL).Get(context.Background(), "user", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user["id"] != "user-1" {
		t.Errorf("user = %v, want the envelope unwrapped", user)
	}
	if _, ok := user["meta"]; ok {
		t.Errorf("user = %v, want only the data object", user)
	}
}

func TestGetJoinsPathOntoBaseURL(t *testing.T) {
	got := newCapture()
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		got.set("path", r.URL.Path)
		_, _ = io.WriteString(w, `{}`)
	})

	// Base URL with a path prefix and a trailing slash, path with a leading one.
	client, err := New(Config{APIKey: "test-key", RestV1URL: srv.URL + "/v1/", HTTPClient: &http.Client{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Get(context.Background(), "/user", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.get("path") != "/v1/user" {
		t.Errorf("path = %q, want /v1/user", got.get("path"))
	}
}

func TestResourcePathEscapesTheId(t *testing.T) {
	// url.JoinPath resolves `..` and passes `/` through, so an id that carries
	// either would move the request to a different route. Escaping keeps it a
	// single path segment.
	if got := resourcePath("pods", "a/b"); got != "pods/a%2Fb" {
		t.Errorf("resourcePath = %q, want the slash escaped", got)
	}
	if got := resourcePath("pods", "../../admin"); got != "pods/..%2F..%2Fadmin" {
		t.Errorf("resourcePath = %q, want the traversal escaped", got)
	}
}

func TestATraversalIdStaysOnItsRoute(t *testing.T) {
	got := newCapture()
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		got.set("path", r.URL.EscapedPath())
		_, _ = io.WriteString(w, `{}`)
	})

	client, err := New(Config{APIKey: "test-key", RestV1URL: srv.URL + "/v1", HTTPClient: &http.Client{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetPodV1(context.Background(), "../../admin"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.get("path") != "/v1/pods/..%2F..%2Fadmin" {
		t.Errorf("path = %q, want the id kept inside /v1/pods/", got.get("path"))
	}
}

func TestGetReportsUnauthorized(t *testing.T) {
	srv := testServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"invalid api key"}`)
	})

	_, err := testClient(t, srv.URL).Get(context.Background(), "user", nil)
	if err == nil {
		t.Fatal("expected an error for HTTP 401")
	}
	if !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "invalid api key") {
		t.Errorf("error = %q, want the status and the body", err)
	}
}

func TestGetReportsServerError(t *testing.T) {
	srv := testServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "upstream failure")
	})

	_, err := testClient(t, srv.URL).Get(context.Background(), "user", nil)
	if err == nil {
		t.Fatal("expected an error for HTTP 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %q, want the status", err)
	}
}

func TestGetReportsMalformedJSON(t *testing.T) {
	srv := testServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"id":`)
	})

	_, err := testClient(t, srv.URL).Get(context.Background(), "user", nil)
	if err == nil || !strings.Contains(err.Error(), "decoding") {
		t.Fatalf("error = %v, want a decode error", err)
	}
}

func TestGetReportsTransportFailure(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()

	_, err := testClient(t, url).Get(context.Background(), "user", nil)
	if err == nil || !strings.Contains(err.Error(), "/user") {
		t.Fatalf("error = %v, want a transport error naming the URL", err)
	}
}
