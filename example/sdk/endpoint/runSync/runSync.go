package main

import (
	"fmt"
	"time"

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
	now := time.Now()
	jobInput := rpEndpoint.RunSyncInput{
		JobInput: &rpEndpoint.JobInput{
			Input: map[string]interface{}{"mock_delay": 95},
		},
		Timeout: sdk.Int(120),
	}
	output, err := endpoint.RunSync(&jobInput)
	if err != nil {
		fmt.Println(time.Since(now).Seconds())
		panic(err)
	}
	fmt.Println(time.Since(now).Seconds())
	fmt.Println("output: ", *output.Status)

}
