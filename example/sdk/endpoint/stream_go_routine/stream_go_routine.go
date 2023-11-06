package main

import (
	"encoding/json"
	"fmt"
	"sync"

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

	request, err := endpoint.Run(&rpEndpoint.RunInput{
		JobInput: &rpEndpoint.JobInput{
			Input: map[string]interface{}{
				"mock_return": []string{
					"value1",
					"value2",
					"value3",
					"value4",
					"value5",
					"value6",
					"value7",
					"value8",
					"value9",
					"value10",
				},
				"mock_delay": 10,
				"mock_error": false,
				"mock_crash": false,
			},
		},
	})
	if err != nil {
		panic(err)
	}

	streamChan := make(chan rpEndpoint.StreamOutput, 100)

	var wg sync.WaitGroup
	wg.Add(1)
	// process data as soon as it is received
	go processData(streamChan, &wg)

	err = endpoint.Stream(&rpEndpoint.StreamInput{Id: request.Id}, streamChan)
	if err != nil {
		panic(err)
	}
	wg.Wait()
}

func processData(op chan rpEndpoint.StreamOutput, wg *sync.WaitGroup) {
	defer wg.Done()
	for data := range op {
		dt, _ := json.Marshal(data)
		fmt.Printf("output:%s\n", dt)
	}
}
