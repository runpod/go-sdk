package legacy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// resourcePath builds the path of one typed resource route. The id is escaped
// because url.JoinPath resolves `..` and passes `/` straight through, so a
// caller-supplied id could otherwise send the request to a different route —
// "../../admin" reaches /admin, off the /v1 prefix entirely.
func resourcePath(kind, id string) string {
	return kind + "/" + url.PathEscape(id)
}

// Get issues a REST v1 GET against path (relative to the configured base URL)
// and returns the decoded object. A `{"data": {...}}` envelope is unwrapped,
// matching how the 1.x provider read these routes; a bare object is returned
// as-is.
//
// Only an object-valued `data` is unwrapped here, where unwrapInto — the typed
// path — unwraps any `data`, because a bare array is what the billing routes
// answer and no map can hold one. Unwrapping keeps the envelope's `data` and
// nothing else, so a real v1 object carrying its own object-valued `data`
// member would lose every sibling field; no route this package calls returns
// one, and the 1.x provider read them the same way.
func (c *Client) Get(ctx context.Context, path string, params map[string]string) (map[string]any, error) {
	endpoint, err := c.endpoint(path, params)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building the REST v1 request failed: %w", err)
	}
	c.authorize(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling %s failed: %w", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if !isSuccess(resp.StatusCode) {
		return nil, statusError(resp)
	}

	var decoded map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBody)).Decode(&decoded); err != nil {
		return nil, &DecodeError{Err: err}
	}
	if data, ok := decoded["data"].(map[string]any); ok {
		return data, nil
	}
	return decoded, nil
}

// getInto issues a REST v1 GET and decodes the payload into out, which must be
// a pointer. It is the typed counterpart of Get: the billing routes answer
// with a bare JSON array, which no map can hold.
func (c *Client) getInto(ctx context.Context, path string, params map[string]string, out any) error {
	endpoint, err := c.endpoint(path, params)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("building the REST v1 request failed: %w", err)
	}
	c.authorize(req)

	return c.do(req, endpoint, out)
}

// patchInto issues a REST v1 PATCH with a JSON body and decodes the payload
// into out. A nil body field is omitted by its `omitempty` tag, so only the
// fields a caller set are sent.
func (c *Client) patchInto(ctx context.Context, path string, body, out any) error {
	endpoint, err := c.endpoint(path, nil)
	if err != nil {
		return err
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encoding the REST v1 request failed: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("building the REST v1 request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.authorize(req)

	return c.do(req, endpoint, out)
}

func (c *Client) endpoint(path string, params map[string]string) (string, error) {
	endpoint, err := url.JoinPath(c.restV1URL, path)
	if err != nil {
		return "", fmt.Errorf("building the REST v1 URL for %q failed: %w", path, err)
	}
	if len(params) > 0 {
		query := url.Values{}
		for k, v := range params {
			query.Set(k, v)
		}
		endpoint += "?" + query.Encode()
	}
	return endpoint, nil
}

func (c *Client) do(req *http.Request, endpoint string, out any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("calling %s failed: %w", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if !isSuccess(resp.StatusCode) {
		return statusError(resp)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return fmt.Errorf("reading the REST v1 response failed: %w", err)
	}
	// A 204, or any other success with no body, landed the write and has
	// nothing to report back; the caller keeps what it planned. Live v1 PATCH
	// semantics are unverified, so this is tolerance rather than an observed
	// shape.
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	return unwrapInto(body, out)
}

// isSuccess accepts any 2xx: a shim write that answers 202 or 204 landed, and
// failing the apply over the status code would undo a change the host already
// made.
func isSuccess(status int) bool {
	return status >= http.StatusOK && status < http.StatusMultipleChoices
}

// unwrapInto decodes body into out, unwrapping a `{"data": ...}` envelope of
// any type when the response carries one — the same tolerance Get applies to an
// object-valued one, kept because the 1.x provider read these routes through an
// envelope-unwrapping helper and the live routes are not verified either way.
// As in Get, only `data` survives the unwrap. A body that will not decode comes
// back as *DecodeError, so a caller can tell a write that landed and answered
// something unreadable from a write that failed.
func unwrapInto(body []byte, out any) error {
	payload := body
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err == nil {
		if data, ok := envelope["data"]; ok {
			payload = data
		}
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return &DecodeError{Err: err}
	}
	return nil
}
