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

	err = endpoint.Stream(&rpEndpoint.StreamInput{Id: request.Id}, streamChan)
	if err != nil {
		// timeout reached, if we want to get the data that has been streamed
		if err.Error() == "ctx timeout reached" {
			for data := range streamChan {
				dt, _ := json.Marshal(data)
				fmt.Printf("output:%s\n", dt)
			}
		}
		panic(err)
	}

	for data := range streamChan {
		dt, _ := json.Marshal(data)
		fmt.Printf("output:%s\n", dt)
	}

}
