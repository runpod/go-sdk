package main

import (
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
	jobInput := rpEndpoint.RunInput{
		JobInput: &rpEndpoint.JobInput{
			Input: map[string]interface{}{"mock_delay": 95},
		},
		RequestTimeout: sdk.Int(120),
	}
	output, err := endpoint.Run(&jobInput)
	if err != nil {
		panic(err)
	}
	fmt.Println("output: ", *output.Id, *output.Status)

}
