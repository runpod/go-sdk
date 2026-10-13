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
	// a status code, so check the typed status field for it.
	gpus, err := client.Catalog.ListGpuTypesWithResponse(ctx, nil)
	if err != nil {
		log.Fatal(err)
	}
	if gpus.JSON200 == nil {
		log.Fatalf("catalog: HTTP %d: %s", gpus.HTTPResponse.StatusCode, gpus.Body)
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
	if created.JSON201 == nil {
		log.Fatalf("create pod: HTTP %d: %s", created.HTTPResponse.StatusCode, created.Body)
	}
	fmt.Println(created.JSON201.Id)
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
		if page.JSON200 == nil {
			log.Fatalf("list pods: HTTP %d: %s", page.HTTPResponse.StatusCode, page.Body)
		}
		all = append(all, page.JSON200.Pods...)
		if !page.JSON200.Pagination.HasNextPage {
			break
		}
		params.Cursor = page.JSON200.Pagination.NextCursor
	}
	fmt.Println(len(all))
}
