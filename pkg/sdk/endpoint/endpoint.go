package endpoint

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/runpod/go-sdk/pkg/sdk/config"
)

const slsEndpoint = "https://api.runpod.ai/v2/"

func New(cf *config.Config, input *Option) (*Endpoint, error) {
	if cf.ApiKey == nil {
		return nil, fmt.Errorf("api key is required")
	}
	if input.EndpointId == nil {
		return nil, fmt.Errorf("endpoint id is required")
	}
	return &Endpoint{apiKey: cf.ApiKey, EndpointId: input.EndpointId}, nil
}

func (ep *Endpoint) Run(input *RunInput) (*RunOutput, error) {
	var timeout int
	if input.RequestTimeout != nil {
		timeout = *input.RequestTimeout
	} else {
		timeout = 3
	}

	reqBody, err := json.Marshal(input.JobInput)
	if err != nil {
		return nil, fmt.Errorf("json marshal error: %s", err)
	}

	url := slsEndpoint + *ep.EndpointId + "/run"

	var result RunOutput
	err = getApiResponse(apiRequestInput{url: &url, reqBody: reqBody, token: ep.apiKey, timeout: &timeout}, &result)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

func (ep *Endpoint) RunSync(input *RunSyncInput) (*RunSyncOutput, error) {

	wait := 90 * 1000
	var timeout, reqTimeout int

	if input.Timeout != nil {
		timeout = *input.Timeout
	} else {
		timeout = 120
	}

	if timeout >= 90 {
		reqTimeout = 90 + 2
	} else {
		wait = timeout * 1000
		reqTimeout = timeout + 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout+3)*time.Second)
	defer cancel()

	url, err := getRunSyncURL(ep, wait)
	if err != nil {
		return nil, err
	}

	reqBody, err := json.Marshal(input.JobInput)
	if err != nil {
		return nil, fmt.Errorf("json marshal error: %s", err)
	}

	var result RunSyncOutput
	err = getApiResponse(apiRequestInput{url: url, reqBody: reqBody, token: ep.apiKey, timeout: &reqTimeout}, &result)
	if err != nil {
		return nil, err
	}

	if result.Status != nil && (*result.Status == "COMPLETED" || *result.Status == "FAILED") {
		return &result, nil
	} else if result.Error != nil {
		return &result, nil
	}

	// request is in queue so fetch using statusSync
	statusSyncURL, err := getStatusSyncURL(ep, result.Id, wait)
	if err != nil {
		return nil, err
	}

	for {
		select {
		case <-ctx.Done():
			return &result, fmt.Errorf("timeout reached")
		default:
			err := statusSyncApiCall(ctx, ep, statusSyncURL, &reqTimeout, &result)
			if err != nil {
				return &result, err
			}
			if result.Status != nil && (*result.Status == "COMPLETED" || *result.Status == "FAILED") {
				return &result, nil
			} else if result.Error != nil {
				return &result, nil
			}
		}
	}

	return &result, nil
}

func getStatusSyncURL(ep *Endpoint, id *string, wait int) (*string, error) {
	queryParams := url.Values{}
	queryParams.Add("wait", strconv.Itoa(wait))

	baseURL := slsEndpoint + *ep.EndpointId + "/status-sync/" + *id

	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("url parse error: %s", err)
	}
	u.RawQuery = queryParams.Encode()
	url := u.String()
	return &url, nil
}

func getRunSyncURL(ep *Endpoint, wait int) (*string, error) {

	queryParams := url.Values{}
	queryParams.Add("wait", strconv.Itoa(wait))

	baseURL := slsEndpoint + *ep.EndpointId + "/runsync"

	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("url parse error: %s", err)
	}
	u.RawQuery = queryParams.Encode()
	url := u.String()

	return &url, nil
}

func (ep *Endpoint) StatusSync(input *StatusSyncInput) (*StatusSyncOutput, error) {
	if input.Id == nil {
		return nil, fmt.Errorf("endpoint id is required")
	}

	wait := 90 * 1000
	var timeout, reqTimeout int

	if input.Timeout != nil {
		timeout = *input.Timeout
	} else {
		timeout = 120
	}

	if timeout >= 90 {
		reqTimeout = 90 + 2
	} else {
		wait = timeout * 1000
		reqTimeout = timeout + 2
	}

	statusSyncURL, err := getStatusSyncURL(ep, input.Id, wait)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout+3)*time.Second)
	defer cancel()
	var result StatusSyncOutput

	for {
		select {
		case <-ctx.Done():
			return &result, fmt.Errorf("timeout reached")
		default:

			err := statusSyncApiCall(ctx, ep, statusSyncURL, &reqTimeout, &result)
			if err != nil {
				return &result, err
			}
			if result.Status != nil && (*result.Status == "COMPLETED" || *result.Status == "FAILED") {
				return &result, nil
			} else if result.Error != nil {
				return &result, nil
			}
		}
	}

	return &result, nil
}

func statusSyncApiCall(ctx context.Context, ep *Endpoint, url *string, reqTimeout *int, result interface{}) error {
	done := make(chan bool)
	var err error

	go func() {
		err = getApiResponse(apiRequestInput{url: url, reqBody: nil, token: ep.apiKey, timeout: reqTimeout}, &result)
		if err != nil {
			done <- false
			return
		}
		done <- true
	}()

	select {
	case <-ctx.Done():
		return fmt.Errorf("ctx timeout reached")
	case <-done:
		if err != nil {
			return err
		}
		return nil
	}

}

func (ep *Endpoint) GetStatus(input *GetStatusInput) (*GetStatusOutput, error) {
	if input.Id == nil {
		return nil, fmt.Errorf("endpoint id is required")
	}
	var timeout int
	if input.RequestTimeout != nil {
		timeout = *input.RequestTimeout
	} else {
		timeout = 3
	}

	url := slsEndpoint + *ep.EndpointId + "/status/" + *input.Id

	var result GetStatusOutput
	err := getApiResponse(apiRequestInput{
		url:     &url,
		reqBody: nil,
		token:   ep.apiKey,
		timeout: &timeout,
	}, &result)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

func getApiResponse(input apiRequestInput, result interface{}) error {
	req, err := http.NewRequest("POST", *input.url, bytes.NewBuffer(input.reqBody))
	if err != nil {
		return fmt.Errorf("http request create error: %s", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+*input.token)

	client := &http.Client{Timeout: time.Second * time.Duration(*input.timeout)}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("sls request error: %s", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("response status %s", resp.Status)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("io read error: %s", err)
	}

	err = json.Unmarshal(respBody, &result)
	if err != nil {
		return fmt.Errorf("json decoder error: %s", err)
	}
	return nil
}
