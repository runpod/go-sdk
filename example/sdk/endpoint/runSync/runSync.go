package main

import (
	"encoding/json"
	"fmt"

	"github.com/runpod/go-sdk/pkg/sdk"
	"github.com/runpod/go-sdk/pkg/sdk/config"
	rpEndpoint "github.com/runpod/go-sdk/pkg/sdk/endpoint"
)

func main() {

	endpoint, err := rpEndpoint.New(
		&config.Config{ApiKey: sdk.String("API_KEY")},
		&rpEndpoint.Option{EndpointId: sdk.String("ENDPOINT_ID")},
	)
	if err != nil {
		panic(err)
	}
	jobInput := rpEndpoint.RunSyncInput{
		JobInput: &rpEndpoint.JobInput{
			Input: map[string]interface{}{"mock_delay": 10},
		},
		Timeout: sdk.Int(120),
	}
	output, err := endpoint.RunSync(&jobInput)
	if err != nil {
		panic(err)
	}
	data, _ := json.Marshal(output)
	fmt.Printf("output: %s\n", data)
}
