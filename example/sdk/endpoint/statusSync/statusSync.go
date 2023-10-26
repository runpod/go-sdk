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
	input := rpEndpoint.StatusSyncInput{
		Id:      sdk.String("63c140e6-888f-4d5b-857c-90db79b9f67e-u1"),
		Timeout: sdk.Int(100),
	}
	now := time.Now()
	output, err := endpoint.StatusSync(&input)
	if err != nil {
		fmt.Println(time.Since(now).Seconds())
		fmt.Println("output: ", output)

		fmt.Println("output: ", *output.Id, *output.Status)

		panic(err)
	}
	fmt.Println(time.Since(now).Seconds())
	fmt.Println("output: ", *output.Id, *output.Status)

}
