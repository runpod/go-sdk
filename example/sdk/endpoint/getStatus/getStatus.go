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
	input := rpEndpoint.StatusInput{
		Id: sdk.String("30edb8b9-2b8d-4977-af7a-85fd91f51a12-u1"),
	}
	output, err := endpoint.Status(&input)
	if err != nil {
		panic(err)
	}
	dt, _ := json.Marshal(output)
	fmt.Printf("output:%s\n", dt)
}
