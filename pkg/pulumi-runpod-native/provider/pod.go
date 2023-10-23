package provider

import p "github.com/pulumi/pulumi-go-provider"

type Pod struct{}

type PodArgs struct {
	GpuTypeId string       `pulumi:"gpuTypeId"`
	GpuCount  int          `pulumi:"gpuCount"`
	CloudType PodCloudType `pulumi:"cloudType"`
}

type PodCloudType string

const (
	ALL       PodCloudType = "ALL"
	SECURE    PodCloudType = "SECURE"
	COMMUNITY PodCloudType = "COMMUNITY"
)

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
