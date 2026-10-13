package legacy

import (
	"context"
	"encoding/json"
)

// PodBillingQueryV1 filters the pod billing read behind the deprecated
// runpod_billing_pod data source.
//
// The query parameters are exactly the ones v1.0.8 sent (bucketSize, endTime
// and the one id filter per route; it never sent a start time), and an unset
// filter is left off the query string rather than sent empty.
type PodBillingQueryV1 struct {
	BucketSize string
	EndTime    string
	GPUTypeID  string
	PodID      string
}

func (q PodBillingQueryV1) params() map[string]string {
	return billingParams(q.BucketSize, q.EndTime, map[string]string{
		"gpuTypeId": q.GPUTypeID,
		"podId":     q.PodID,
	})
}

// PodBillingRecordV1 is one bucket of the /billing/pods array.
//
// Every field is a pointer so a missing one becomes a null attribute: v1.0.8
// read amount and time with unchecked type assertions that panicked when
// either was absent. diskSpaceBilledGb and timeBilledMs are float64 because
// that is what JSON gives and v1.0.8 truncated with int64(val). The data
// sources truncate the same way, and a fractional value would fail an int64
// decode outright.
type PodBillingRecordV1 struct {
	Amount            *float64 `json:"amount"`
	DiskSpaceBilledGb *float64 `json:"diskSpaceBilledGb"`
	EndpointID        *string  `json:"endpointId"`
	GPUTypeID         *string  `json:"gpuTypeId"`
	PodID             *string  `json:"podId"`
	Time              *string  `json:"time"`
	TimeBilledMs      *float64 `json:"timeBilledMs"`
}

// ListPodBillingV1 reads pod billing buckets. The route answers with a bare
// JSON array, which is why these go through getInto rather than Get.
func (c *Client) ListPodBillingV1(ctx context.Context, query PodBillingQueryV1) ([]PodBillingRecordV1, error) {
	records := []PodBillingRecordV1{}
	if err := c.getInto(ctx, "billing/pods", query.params(), &records); err != nil {
		return nil, err
	}
	return records, nil
}

// NetworkVolumeBillingQueryV1 filters the network-volume billing read behind
// the deprecated runpod_billing_network_volume data source.
type NetworkVolumeBillingQueryV1 struct {
	BucketSize      string
	EndTime         string
	NetworkVolumeID string
}

func (q NetworkVolumeBillingQueryV1) params() map[string]string {
	return billingParams(q.BucketSize, q.EndTime, map[string]string{
		"networkVolumeId": q.NetworkVolumeID,
	})
}

// NetworkVolumeBillingRecordV1 is one bucket of the /billing/networkvolumes
// array; the pointers and float64s are there for the reasons
// PodBillingRecordV1 gives. The sources disagree on the name of the bucket's
// timestamp: the public v1 spec's NetworkVolumeBillingRecords, runpodctl and
// the 1.x provider all read time, while a backend-shaped fixture from review
// carries startDate. The handler source is not available to settle it, so
// Time decodes from startDate when present and from time otherwise.
type NetworkVolumeBillingRecordV1 struct {
	Amount            *float64 `json:"amount"`
	DiskSpaceBilledGb *float64 `json:"diskSpaceBilledGb"`
	NetworkVolumeID   *string  `json:"networkVolumeId"`
	Time              *string  `json:"time"`
	TimeBilledMs      *float64 `json:"timeBilledMs"`
}

// UnmarshalJSON fills Time from startDate, falling back to time.
func (r *NetworkVolumeBillingRecordV1) UnmarshalJSON(data []byte) error {
	type record NetworkVolumeBillingRecordV1
	var wire struct {
		record
		StartDate *string `json:"startDate"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*r = NetworkVolumeBillingRecordV1(wire.record)
	if wire.StartDate != nil {
		r.Time = wire.StartDate
	}
	return nil
}

// ListNetworkVolumeBillingV1 reads network-volume billing buckets.
func (c *Client) ListNetworkVolumeBillingV1(ctx context.Context, query NetworkVolumeBillingQueryV1) ([]NetworkVolumeBillingRecordV1, error) {
	records := []NetworkVolumeBillingRecordV1{}
	if err := c.getInto(ctx, "billing/networkvolumes", query.params(), &records); err != nil {
		return nil, err
	}
	return records, nil
}

// EndpointBillingQueryV1 filters the endpoint billing read behind the
// deprecated runpod_billing_endpoint data source.
type EndpointBillingQueryV1 struct {
	BucketSize string
	EndTime    string
	EndpointID string
}

func (q EndpointBillingQueryV1) params() map[string]string {
	return billingParams(q.BucketSize, q.EndTime, map[string]string{
		"endpointId": q.EndpointID,
	})
}

// EndpointBillingRecordV1 is one bucket of the /billing/endpoints array; the
// pointers and float64s are there for the reasons PodBillingRecordV1 gives.
type EndpointBillingRecordV1 struct {
	Amount       *float64 `json:"amount"`
	EndpointID   *string  `json:"endpointId"`
	Time         *string  `json:"time"`
	TimeBilledMs *float64 `json:"timeBilledMs"`
}

// ListEndpointBillingV1 reads serverless-endpoint billing buckets.
func (c *Client) ListEndpointBillingV1(ctx context.Context, query EndpointBillingQueryV1) ([]EndpointBillingRecordV1, error) {
	records := []EndpointBillingRecordV1{}
	if err := c.getInto(ctx, "billing/endpoints", query.params(), &records); err != nil {
		return nil, err
	}
	return records, nil
}

// billingParams keeps the v1.0.8 rule that an unset filter is left off the
// query string entirely rather than sent empty.
func billingParams(bucketSize, endTime string, filters map[string]string) map[string]string {
	params := map[string]string{}
	if bucketSize != "" {
		params["bucketSize"] = bucketSize
	}
	if endTime != "" {
		params["endTime"] = endTime
	}
	for name, value := range filters {
		if value != "" {
			params[name] = value
		}
	}
	return params
}
