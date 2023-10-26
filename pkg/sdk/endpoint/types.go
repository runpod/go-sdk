package endpoint

type Endpoint struct {
	apiKey *string

	// EndpointId where the job will be executed
	EndpointId *string
}

type Option struct {
	EndpointId *string `json:"endpointId" required:"true"`
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
	Id     *string
	Status *string
}

type RunSyncOutput struct {
	DelayTime     *int
	Error         *string
	ExecutionTime *int
	Id            *string
	Output        *interface{}
	Retries       *int
	Status        *string
}

type apiRequestInput struct {
	url     *string
	reqBody []byte
	token   *string
	timeout *int
}

type GetStatusInput struct {
	Id             *string `json:"id" required:"true"`
	RequestTimeout *int    `default:"3"`
}

type GetStatusOutput struct {
	DelayTime     *int
	Error         *string
	ExecutionTime *int
	Id            *string
	Output        *interface{}
	Retries       *int
	Status        *string
}

type StatusSyncInput struct {
	Id      *string `json:"id" required:"true"`
	Timeout *int    `default:"90"`
}

type StatusSyncOutput struct {
	DelayTime     *int
	Error         *string
	ExecutionTime *int
	Id            *string
	Output        *interface{}
	Retries       *int
	Status        *string
}
