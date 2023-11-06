# go-sdk

go client sdk for runpod

# Example Usage

```go
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
	data, _ := json.Marshal(output)
	fmt.Printf("output: %s\n", data)
}

```

# Using Endpoints

Once an endpoint has been created, you can send requests to the queue:

```go
output, err := endpoint.Run(&rpEndpoint.RunInput{
    JobInput: &rpEndpoint.JobInput{
        Input: map[string]interface{}{"mock_delay": 95},
    },
})
```

You can check on the status of this request once you have the id:

```go
input := rpEndpoint.GetStatusInput{
    Id: sdk.String("30edb8b9-2b8d-4977-af7a-85fd91f51a12-u1"),
}
output, err := endpoint.GetStatus(&input)
```

If you don't want to manage polling for request completion yourself, you can simply call `runSync`, which will enqueue the request and then poll until the request completes, fails or times out.

```go
jobInput := rpEndpoint.RunSyncInput{
    JobInput: &rpEndpoint.JobInput{
        Input: map[string]interface{}{"mock_delay": 10},
    },
    Timeout: sdk.Int(120),
}
output, err := endpoint.RunSync(&jobInput)
```

If you have the id of a request, you can cancel it if it's taking too long or no longer necessary:

```go
input := rpEndpoint.CancelRequestInput{
    Id: sdk.String("30edb8b9-2b8d-4977-af7a-85fd91f51a12-u1"),
}
output, err := endpoint.CancelRequest(&input)
```

For long running applications or troubleshooting, you may want to check the health of the endpoint workers:

```go
output, err := endpoint.GetHealth(&input)
```

Streaming is also supported with channels. refer to stream_go_routine example to use results as soon as it streams.

```go
streamChan := make(chan rpEndpoint.StreamOutput, 100)
err = endpoint.Stream(&rpEndpoint.StreamInput{Id: request.Id}, streamChan)
for data := range streamChan {
    dt, _ := json.Marshal(data)
    fmt.Printf("output:%s\n", dt)
}
```
