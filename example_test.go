package runpod_test

import (
	"context"
	"fmt"
	"log"

	runpod "github.com/runpod/go-sdk/v2"
	"github.com/runpod/go-sdk/v2/lib/pods"
	"github.com/runpod/go-sdk/v2/lib/types"
)

// Example is the README's usage section, compiled. It has no output comment, so
// `go test` builds it and never runs it. The point is that a regeneration that
// moves a request shape breaks the build here instead of breaking the first
// code a new user copies.
func Example() {
	ctx := context.Background()

	client, err := runpod.New() // key from RUNPOD_API_KEY
	if err != nil {
		log.Fatal(err)
	}

	// The *WithResponse methods return a response wrapper: err covers
	// transport, body-read and decode failures. An API-level error arrives as
	// a status code, which CheckResponse turns into an *APIError.
	gpus, err := client.Catalog.ListGpuTypesWithResponse(ctx, nil)
	if err != nil {
		log.Fatal(err)
	}
	if err := runpod.CheckResponse(gpus.HTTPResponse, gpus.Body); err != nil {
		log.Fatal(err)
	}
	for _, gpu := range gpus.JSON200.Gpus {
		fmt.Println(gpu.Id, gpu.Memory)
	}

	created, err := client.Pods.CreatePodWithResponse(ctx, pods.CreatePodJSONRequestBody{
		Name:  "training-pod",
		Image: runpod.Ptr("runpod/pytorch:2.8.0-py3.11-cuda12.8.1-cudnn-devel-ubuntu22.04"),
		Gpu:   &types.CreateGpuConfig{Id: "NVIDIA GeForce RTX 4090", Count: runpod.Ptr(1)},
	})
	if err != nil {
		log.Fatal(err)
	}
	if err := runpod.CheckResponse(created.HTTPResponse, created.Body); err != nil {
		log.Fatal(err)
	}
	// A 201 is the pod; a 202 (only with queue set) means it is queued.
	if pod := created.JSON201; pod != nil {
		fmt.Println(pod.Id)
	}
}

// Example_pagination is the README's paging block, compiled on the same terms.
// A list call without a cursor loop returns one page, not the whole set, so
// this is the shape a caller that wants everything has to write.
func Example_pagination() {
	ctx := context.Background()

	client, err := runpod.New()
	if err != nil {
		log.Fatal(err)
	}

	var all []types.Pod
	params := &pods.ListPodsParams{}
	for {
		page, err := client.Pods.ListPodsWithResponse(ctx, params)
		if err != nil {
			log.Fatal(err)
		}
		if err := runpod.CheckResponse(page.HTTPResponse, page.Body); err != nil {
			log.Fatal(err)
		}
		all = append(all, page.JSON200.Pods...)
		if !page.JSON200.Pagination.HasNextPage {
			break
		}
		params.Cursor = page.JSON200.Pagination.NextCursor
	}
	fmt.Println(len(all))
}
