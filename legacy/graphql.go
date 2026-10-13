package legacy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type graphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

type graphQLError struct {
	Message string `json:"message"`
}

type graphQLResponse struct {
	Data   map[string]any `json:"data"`
	Errors []graphQLError `json:"errors"`
}

// Query posts a GraphQL document and returns its top-level "data" object.
// GraphQL reports field errors with HTTP 200 and an "errors" array, so those
// come back as a Go error too — a caller never has to inspect the envelope.
func (c *Client) Query(ctx context.Context, query string, variables map[string]any) (map[string]any, error) {
	payload, err := json.Marshal(graphQLRequest{Query: query, Variables: variables})
	if err != nil {
		return nil, fmt.Errorf("encoding the GraphQL request failed: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.graphQLURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("building the GraphQL request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.authorize(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling %s failed: %w", c.graphQLURL, err)
	}
	// Read-side body, and nothing here retries or reuses it: the close error
	// carries nothing a caller could act on, so it is dropped explicitly.
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, statusError(resp)
	}

	var decoded graphQLResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBody)).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decoding the GraphQL response failed: %w", err)
	}
	if len(decoded.Errors) > 0 {
		messages := make([]string, 0, len(decoded.Errors))
		for _, e := range decoded.Errors {
			messages = append(messages, e.Message)
		}
		return nil, fmt.Errorf("GraphQL errors: %s", strings.Join(messages, "; "))
	}
	if decoded.Data == nil {
		return nil, fmt.Errorf("GraphQL response carried no data")
	}
	return decoded.Data, nil
}
