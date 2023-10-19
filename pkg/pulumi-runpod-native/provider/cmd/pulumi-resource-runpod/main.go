package main

import (
	"fmt"

	p "github.com/pulumi/pulumi-go-provider"
	runpod "github.com/runpod/go-sdk/pkg/pulumi-runpod-native/provider"
)

// Serve the provider against Pulumi's Provider protocol.
func main() {
	err := p.RunProvider(runpod.Name, runpod.Version, runpod.Provider())
	if err != nil {
		fmt.Println("err:", err.Error())
	}
}
