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

var slsEndpointUrl = "https://api.runpod.ai/v2"

func New(cf *config.Config, input *Option) (*Endpoint, error) {
	if cf.ApiKey == nil {
		return nil, fmt.Errorf("api key is required")
	}
	if input.EndpointId == nil {
		return nil, fmt.Errorf("endpoint id is required")
	}
	if input.EndpointUrl != nil {
		return &Endpoint{apiKey: cf.ApiKey, EndpointId: input.EndpointId, EndpointUrl: input.EndpointUrl}, nil
	}
	return &Endpoint{apiKey: cf.ApiKey, EndpointId: input.EndpointId, EndpointUrl: &slsEndpointUrl}, nil
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

	url := *ep.EndpointUrl + "/" + *ep.EndpointId + "/run"

	respBody, err := getApiResponse(apiRequestInput{method: "POST", url: &url, reqBody: reqBody, token: ep.apiKey, timeout: &timeout})
	if err != nil {
		return nil, err
	}
	var result RunOutput
	err = json.Unmarshal(respBody, &result)
	if err != nil {
		return nil, fmt.Errorf("json decoder error: %s", err)
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
	respBody, err := getApiResponse(apiRequestInput{method: "POST", url: url, reqBody: reqBody, token: ep.apiKey, timeout: &reqTimeout})
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(respBody, &result)
	if err != nil {
		return nil, fmt.Errorf("json decoder error: %s", err)
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
			respBody, err := statusSyncApiCall(ctx, ep, statusSyncURL, &reqTimeout)
			if err != nil {
				return &result, err
			}
			err = json.Unmarshal(respBody, &result)
			if err != nil {
				return &result, fmt.Errorf("json decoder error: %s", err)
			}
			err = json.Unmarshal(respBody, &result)
			if err != nil {
				return &result, fmt.Errorf("json decoder error: %s", err)
			}
			if result.Status != nil && (*result.Status == "COMPLETED" || *result.Status == "FAILED") {
				return &result, nil
			} else if result.Error != nil {
				return &result, nil
			}
		}
	}

	// return &result, nil
}

func getStatusSyncURL(ep *Endpoint, id *string, wait int) (*string, error) {
	queryParams := url.Values{}
	queryParams.Add("wait", strconv.Itoa(wait))

	baseURL := *ep.EndpointUrl + "/" + *ep.EndpointId + "/status-sync/" + *id

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

	baseURL := *ep.EndpointUrl + "/" + *ep.EndpointId + "/runsync"

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
		return nil, fmt.Errorf("job id is required")
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
			respBody, err := statusSyncApiCall(ctx, ep, statusSyncURL, &reqTimeout)
			if err != nil {
				return &result, err
			}
			err = json.Unmarshal(respBody, &result)
			if err != nil {
				return &result, fmt.Errorf("json decoder error: %s", err)
			}
			if result.Status != nil && (*result.Status == "COMPLETED" || *result.Status == "FAILED") {
				return &result, nil
			} else if result.Error != nil {
				return &result, nil
			}
		}
	}
}

func statusSyncApiCall(ctx context.Context, ep *Endpoint, url *string, reqTimeout *int) ([]byte, error) {
	done := make(chan bool)
	var err error
	var respBody []byte
	go func() {
		respBody, err = getApiResponse(apiRequestInput{method: "POST", url: url, token: ep.apiKey, timeout: reqTimeout})
		if err != nil {
			done <- false
			return
		}
		done <- true
	}()

	select {
	case <-ctx.Done():
		return respBody, fmt.Errorf("ctx timeout reached")
	case <-done:
		if err != nil {
			return respBody, err
		}
		return respBody, nil
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

	url := *ep.EndpointUrl + "/" + *ep.EndpointId + "/status/" + *input.Id

	var result GetStatusOutput
	respBody, err := getApiResponse(apiRequestInput{
		method:  "POST",
		url:     &url,
		token:   ep.apiKey,
		timeout: &timeout,
	})
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(respBody, &result)
	if err != nil {
		return &result, fmt.Errorf("json decoder error: %s", err)
	}

	return &result, nil
}

func getApiResponse(input apiRequestInput) ([]byte, error) {
	var result []byte
	req, err := http.NewRequest(input.method, *input.url, bytes.NewBuffer(input.reqBody))
	if err != nil {
		return result, fmt.Errorf("http request create error: %s", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+*input.token)

	client := &http.Client{Timeout: time.Second * time.Duration(*input.timeout)}
	resp, err := client.Do(req)
	if err != nil {
		return result, fmt.Errorf("sls request error: %s", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("response status %s", resp.Status)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return result, fmt.Errorf("io read error: %s", err)
	}

	return respBody, nil
}

func (ep *Endpoint) GetHealth(input *GetHealthInput) (*GetHealthOutput, error) {
	var timeout int
	if input.RequestTimeout != nil {
		timeout = *input.RequestTimeout
	} else {
		timeout = 3
	}

	url := *ep.EndpointUrl + "/" + *ep.EndpointId + "/health"

	var result GetHealthOutput
	respBody, err := getApiResponse(apiRequestInput{
		method:  "GET",
		url:     &url,
		token:   ep.apiKey,
		timeout: &timeout,
	})
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(respBody, &result)
	if err != nil {
		return &result, fmt.Errorf("json decoder error: %s", err)
	}

	return &result, nil
}

func (ep *Endpoint) PurgeQueue(input *PurgeQueueInput) (*PurgeQueueOutput, error) {
	var timeout int
	if input.RequestTimeout != nil {
		timeout = *input.RequestTimeout
	} else {
		timeout = 3
	}

	url := *ep.EndpointUrl + "/" + *ep.EndpointId + "/purge-queue"

	var result PurgeQueueOutput
	respBody, err := getApiResponse(apiRequestInput{
		method:  "POST",
		url:     &url,
		token:   ep.apiKey,
		timeout: &timeout,
	})
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(respBody, &result)
	if err != nil {
		return &result, fmt.Errorf("json decoder error: %s", err)
	}
	return &result, nil
}

func (ep *Endpoint) CancelRequest(input *CancelRequestInput) (*CancelRequestOutput, error) {
	if input.Id == nil {
		return nil, fmt.Errorf("job id is required")
	}

	var timeout int
	if input.RequestTimeout != nil {
		timeout = *input.RequestTimeout
	} else {
		timeout = 3
	}

	url := *ep.EndpointUrl + "/" + *ep.EndpointId + "/cancel/" + *input.Id

	var result CancelRequestOutput
	respBody, err := getApiResponse(apiRequestInput{
		method:  "POST",
		url:     &url,
		token:   ep.apiKey,
		timeout: &timeout,
	})
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(respBody, &result)
	if err != nil {
		return &result, fmt.Errorf("json decoder error: %s", err)
	}
	return &result, nil
}

func (ep *Endpoint) Stream(input *StreamInput, outputChan chan<- StreamOutput) error {
	if input.Id == nil {
		return fmt.Errorf("job id is required")
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

	queryParams := url.Values{}
	queryParams.Add("wait", strconv.Itoa(wait))

	baseURL := *ep.EndpointUrl + "/" + *ep.EndpointId + "/stream/" + *input.Id

	u, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("url parse error: %s", err)
	}
	u.RawQuery = queryParams.Encode()
	url := u.String()

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout+3)*time.Second)
	defer cancel()

	defer func() {
		if outputChan != nil {
			close(outputChan)
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("ctx timeout reached")
		default:
			result, err := streamApiCall(ctx, ep, &url, &reqTimeout, outputChan)
			if err != nil {
				return err
			}
			if result.Status != nil && (*result.Status == "COMPLETED" || *result.Status == "FAILED") {
				return nil
			}
		}
	}

	return nil

}

func streamApiCall(ctx context.Context, ep *Endpoint, url *string, reqTimeout *int, outputChan chan<- StreamOutput) (StreamOutput, error) {
	done := make(chan bool)
	var err error
	var result StreamOutput
	var respBody []byte
	go func() {
		respBody, err = getApiResponse(apiRequestInput{method: "POST", url: url, token: ep.apiKey, timeout: reqTimeout})
		if err != nil {
			done <- false
			return
		}
		err = json.Unmarshal(respBody, &result)
		if err != nil {
			done <- false
			return
		}
		outputChan <- result
		done <- true
	}()

	select {
	case <-ctx.Done():
		return result, fmt.Errorf("ctx timeout reached")
	case <-done:
		if err != nil {
			return result, err
		}
		return result, nil
	}
}
