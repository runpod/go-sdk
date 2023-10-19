package provider

import (
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
)

var Version string

const Name string = "runpod"

func Provider() p.Provider {
	return infer.Provider(infer.Options{
		Resources: []infer.InferredResource{
			infer.Resource[*Pod, PodArgs, PodState](),
		},
		Config: infer.Config[*Config](),
	})
}

type Config struct {
	Token string `pulumi:"token"`
}
type Pod struct{}
type PodArgs struct {
	GpuTypeId string `pulumi:"gpuTypeId"`
	GpuCount  int    `pulumi:"gpuCount"`
}
type PodState struct {
	PodArgs
}

func (*Pod) Create(ctx p.Context, name string, input PodArgs, preview bool) (string, PodState, error) {
	state := PodState{PodArgs: input}
	if preview {
		return name, state, nil
	}
	return name, state, nil
}
