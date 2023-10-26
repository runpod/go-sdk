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
	input := rpEndpoint.GetStatusInput{
		Id: sdk.String("3fef28d4-96e9-4e55-b236-5f5dda58b146-u1"),
	}
	output, err := endpoint.GetStatus(&input)
	if err != nil {
		panic(err)
	}
	fmt.Println("output: ", *output.Id, *output.Status, *output.Output)

}
