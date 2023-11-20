package endpoint

type Endpoint struct {
	apiKey *string

	// EndpointId where the job will be executed
	EndpointId  *string
	EndpointUrl *string
}

type Option struct {
	EndpointId  *string `json:"endpointId" required:"true"`
	EndpointUrl *string `json:"endpointUrl" default:"https://api.runpod.ai/v2/"`
}

type RunInput struct {
	// JobInput is the input payload for the job
	JobInput *JobInput

	// RequestTimeout is the maximum time in seconds to wait for the request to complete
	RequestTimeout *int `default:"3"`
}

type RunSyncInput struct {
	// JobInput is the input payload for the job
	JobInput *JobInput

	// RequestTimeout is the maximum time in seconds to wait for the request to complete
	Timeout *int `default:"120"`
}

type JobInput struct {
	Input    map[string]interface{} `json:"input,omitempty"`
	Policy   *Policy                `json:"policy,omitempty"`
	S3Config *S3Config              `json:"s3Config,omitempty"`
	Webhook  *string                `json:"webhook,omitempty"`
}

type S3Config struct {
	AccessId     *string `json:"accessId,omitempty"`
	AccessSecret *string `json:"accessSecret,omitempty"`
	BucketName   *string `json:"bucketName,omitempty"`
	EndpointUrl  *string `json:"endpointUrl,omitempty"`
	ObjectPath   *string `json:"objectPath,omitempty"`
}

type Policy struct {
	TTL              *int `json:"ttl,omitempty"`
	ExecutionTimeout *int `json:"executionTimeout,omitempty"`
}

type RunOutput struct {
	Id     *string `json:"id,omitempty"`
	Status *string `json:"status,omitempty"`
}

type RunSyncOutput struct {
	DelayTime     *int         `json:"delayTime,omitempty"`
	Error         *string      `json:"error,omitempty"`
	ExecutionTime *int         `json:"executionTime,omitempty"`
	Id            *string      `json:"id,omitempty"`
	Output        *interface{} `json:"output,omitempty"`
	Retries       *int         `json:"retries,omitempty"`
	Status        *string      `json:"status,omitempty"`
}

type apiRequestInput struct {
	method  string
	url     *string
	reqBody []byte
	token   *string
	timeout *int
}

type StatusInput struct {
	Id             *string `json:"id" required:"true"`
	RequestTimeout *int    `default:"3"`
}

type StatusOutput struct {
	DelayTime     *int         `json:"delayTime,omitempty"`
	Error         *string      `json:"error,omitempty"`
	ExecutionTime *int         `json:"executionTime,omitempty"`
	Id            *string      `json:"id,omitempty"`
	Output        *interface{} `json:"output,omitempty"`
	Retries       *int         `json:"retries,omitempty"`
	Status        *string      `json:"status,omitempty"`
}

type StatusSyncInput struct {
	Id      *string `json:"id" required:"true"`
	Timeout *int    `default:"120"`
}

type StatusSyncOutput struct {
	DelayTime     *int         `json:"delayTime,omitempty"`
	Error         *string      `json:"error,omitempty"`
	ExecutionTime *int         `json:"executionTime,omitempty"`
	Id            *string      `json:"id,omitempty"`
	Output        *interface{} `json:"output,omitempty"`
	Retries       *int         `json:"retries,omitempty"`
	Status        *string      `json:"status,omitempty"`
}

type HealthInput struct {
	RequestTimeout *int `default:"3"`
}

type HealthOutput struct {
	Workers *HealthWorkerOutput `json:"workers,omitempty"`
	Jobs    *HealthJobOutput    `json:"jobs,omitempty"`
}

type HealthWorkerOutput struct {
	Running      *int `json:"running,omitempty"`
	Idle         *int `json:"idle,omitempty"`
	Initializing *int `json:"initializing,omitempty"`
	Ready        *int `json:"ready,omitempty"`
	Throttled    *int `json:"throttled,omitempty"`
}

type HealthJobOutput struct {
	InProgress *int `json:"inProgress,omitempty"`
	InQueue    *int `json:"inQueue,omitempty"`
	Completed  *int `json:"completed,omitempty"`
	Failed     *int `json:"failed,omitempty"`
	Retried    *int `json:"retried,omitempty"`
}

type PurgeQueueInput struct {
	RequestTimeout *int `default:"3"`
}

type PurgeQueueOutput struct {
	Status  *string `json:"status,omitempty"`
	Removed *int    `json:"removed,omitempty"`
}

type CancelInput struct {
	Id             *string `json:"id" required:"true"`
	RequestTimeout *int    `default:"3"`
}

type CancelOutput struct {
	DelayTime     *int    `json:"delayTime,omitempty"`
	Error         *string `json:"error,omitempty"`
	ExecutionTime *int    `json:"executionTime,omitempty"`
	Id            *string `json:"id,omitempty"`
	Status        *string `json:"status,omitempty"`
}

type StreamInput struct {
	Id      *string `json:"id" required:"true"`
	Timeout *int    `default:"120"`
}

type StreamResult map[string]interface{}
type StreamOutput struct {
	Status *string
	Stream []StreamResult
}
