package main

import (
	"encoding/json"
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
		Id:      sdk.String("sync-435b2842-6b3c-4e7d-b404-24d6a759bc7b-u1"),
		Timeout: sdk.Int(100),
	}
	now := time.Now()
	output, err := endpoint.StatusSync(&input)
	if err != nil {
		panic(err)
	}
	fmt.Println(time.Since(now).Seconds())
	dt, _ := json.Marshal(output)
	fmt.Printf("output:%s\n", dt)
}
