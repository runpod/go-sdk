package runpod

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/runpod/go-sdk/v2/lib/types"
)

// APIError is an error response from the API, carrying the problem details
// (RFC 9457) it sends as application/problem+json.
//
// Title, Detail and Errors are set only when the body parsed as problem JSON.
// A response that did not (a proxy's HTML error page, for example) leaves them
// empty, so check Body or StatusCode before relying on them.
type APIError struct {
	StatusCode int
	Title      string
	Detail     string
	// Errors lists the request-validation failures.
	Errors []string
	// Body is the raw response body, always set.
	Body []byte
}

// Error reads "runpod: HTTP 404 Not Found": the status code and title only.
// Detail and Errors can echo request content, so they stay out of a string
// that may be logged; read them from the fields.
func (e *APIError) Error() string {
	title := e.Title
	if title == "" {
		title = http.StatusText(e.StatusCode)
	}
	return fmt.Sprintf("runpod: HTTP %d %s", e.StatusCode, title)
}

// CheckResponse returns nil for a 2xx response and an *APIError otherwise.
// Pass it a result's HTTPResponse and Body:
//
//	res, err := client.Pods.GetPodWithResponse(ctx, id)
//	if err != nil { ... }
//	if err := runpod.CheckResponse(res.HTTPResponse, res.Body); err != nil { ... }
func CheckResponse(resp *http.Response, body []byte) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	apiErr := &APIError{StatusCode: resp.StatusCode, Body: body}
	var problem types.ErrorResponse
	if json.Unmarshal(body, &problem) == nil {
		apiErr.Title, apiErr.Detail = problem.Title, problem.Detail
		if problem.Errors != nil {
			apiErr.Errors = *problem.Errors
		}
	}
	return apiErr
}
