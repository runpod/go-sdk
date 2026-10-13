package legacy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestQueryReturnsData(t *testing.T) {
	got := newCapture()
	var gotBody atomic.Value

	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		got.set("auth", r.Header.Get("Authorization"))
		got.set("contentType", r.Header.Get("Content-Type"))
		got.set("agent", r.Header.Get("User-Agent"))
		body, _ := io.ReadAll(r.Body)
		var envelope graphQLRequest
		if err := json.Unmarshal(body, &envelope); err != nil {
			t.Errorf("request body is not a GraphQL envelope: %v", err)
		}
		gotBody.Store(envelope)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"machine":{"id":"m-1","gpuTotal":8}}}`)
	})

	data, err := testClient(t, srv.URL).Query(context.Background(),
		"query GetMachine($machineId: String!) { machine { id } }",
		map[string]any{"machineId": "m-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.get("auth") != "Bearer test-key" {
		t.Errorf("Authorization = %q, want Bearer test-key", got.get("auth"))
	}
	if got.get("contentType") != "application/json" {
		t.Errorf("Content-Type = %q", got.get("contentType"))
	}
	if got.get("agent") != DefaultUserAgent {
		t.Errorf("User-Agent = %q, want %q", got.get("agent"), DefaultUserAgent)
	}
	sent, _ := gotBody.Load().(graphQLRequest)
	if !strings.Contains(sent.Query, "GetMachine") {
		t.Errorf("query not forwarded: %q", sent.Query)
	}
	if sent.Variables["machineId"] != "m-1" {
		t.Errorf("variables = %v", sent.Variables)
	}

	machine, ok := data["machine"].(map[string]any)
	if !ok {
		t.Fatalf("data = %v, want a machine object", data)
	}
	if machine["id"] != "m-1" || machine["gpuTotal"].(float64) != 8 {
		t.Errorf("machine = %v", machine)
	}
}

func TestQuerySurfacesGraphQLErrors(t *testing.T) {
	srv := testServer(t, func(w http.ResponseWriter, _ *http.Request) {
		// GraphQL reports field errors with HTTP 200.
		_, _ = io.WriteString(w, `{"data":null,"errors":[{"message":"machine not found"},{"message":"unauthorized"}]}`)
	})

	_, err := testClient(t, srv.URL).Query(context.Background(), "query {}", nil)
	if err == nil {
		t.Fatal("expected an error for a GraphQL errors array")
	}
	if !strings.Contains(err.Error(), "machine not found") || !strings.Contains(err.Error(), "unauthorized") {
		t.Errorf("error = %q, want both GraphQL messages", err)
	}
}

func TestQueryRejectsMissingData(t *testing.T) {
	srv := testServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{}`)
	})

	_, err := testClient(t, srv.URL).Query(context.Background(), "query {}", nil)
	if err == nil || !strings.Contains(err.Error(), "no data") {
		t.Fatalf("error = %v, want a no-data error", err)
	}
}

func TestQueryReportsUnauthorized(t *testing.T) {
	srv := testServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"invalid api key"}`)
	})

	_, err := testClient(t, srv.URL).Query(context.Background(), "query {}", nil)
	if err == nil {
		t.Fatal("expected an error for HTTP 401")
	}
	if !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "invalid api key") {
		t.Errorf("error = %q, want the status and the body", err)
	}
}

func TestQueryReportsServerError(t *testing.T) {
	srv := testServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "upstream failure")
	})

	_, err := testClient(t, srv.URL).Query(context.Background(), "query {}", nil)
	if err == nil {
		t.Fatal("expected an error for HTTP 500")
	}
	if !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "upstream failure") {
		t.Errorf("error = %q, want the status and the body", err)
	}
}

func TestQueryReportsMalformedJSON(t *testing.T) {
	srv := testServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":{"machine":`)
	})

	_, err := testClient(t, srv.URL).Query(context.Background(), "query {}", nil)
	if err == nil || !strings.Contains(err.Error(), "decoding") {
		t.Fatalf("error = %v, want a decode error", err)
	}
}

func TestQueryReportsTransportFailure(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // nothing is listening any more

	_, err := testClient(t, url).Query(context.Background(), "query {}", nil)
	if err == nil || !strings.Contains(err.Error(), url) {
		t.Fatalf("error = %v, want a transport error naming %s", err, url)
	}
}
