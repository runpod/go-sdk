package legacy

import (
	"context"
	"io"
	"net/http"
	"testing"
)

// The billing routes answer with a bare JSON array, which is the whole reason
// getInto exists beside Get: no map can hold one.

func TestListPodBillingV1DecodesABareArray(t *testing.T) {
	got := newCapture()
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		got.set("path", r.URL.Path)
		got.set("query", r.URL.RawQuery)
		_, _ = io.WriteString(w, `[{"amount":1.25,"time":"2026-06-01T00:00:00Z","podId":"pod_abc",`+
			`"gpuTypeId":"NVIDIA A100 80GB PCIe","diskSpaceBilledGb":40,"timeBilledMs":3600000},{}]`)
	})

	records, err := testClient(t, srv.URL).ListPodBillingV1(context.Background(), PodBillingQueryV1{
		BucketSize: "day",
		EndTime:    "2026-06-02T00:00:00Z",
		PodID:      "pod_abc",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.get("path") != "/billing/pods" {
		t.Errorf("path = %q, want /billing/pods", got.get("path"))
	}
	// v1.0.8 never sent a start time, and an unset filter is left off.
	if got.get("query") != "bucketSize=day&endTime=2026-06-02T00%3A00%3A00Z&podId=pod_abc" {
		t.Errorf("query = %q", got.get("query"))
	}
	if len(records) != 2 {
		t.Fatalf("records = %d, want 2", len(records))
	}
	if records[0].Amount == nil || *records[0].Amount != 1.25 || records[0].PodID == nil {
		t.Errorf("record = %+v", records[0])
	}
	// The record with no fields at all is the one v1.0.8 panicked on.
	if records[1].Amount != nil || records[1].Time != nil {
		t.Errorf("an empty record must decode to nil fields: %+v", records[1])
	}
}

func TestListNetworkVolumeBillingV1UnwrapsEnvelope(t *testing.T) {
	srv := testServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"amount":0.5,"networkVolumeId":"netvol_abc"}]}`)
	})

	records, err := testClient(t, srv.URL).ListNetworkVolumeBillingV1(context.Background(),
		NetworkVolumeBillingQueryV1{NetworkVolumeID: "netvol_abc"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 || records[0].NetworkVolumeID == nil || *records[0].NetworkVolumeID != "netvol_abc" {
		t.Fatalf("records = %+v, want the envelope unwrapped", records)
	}
}

func TestListNetworkVolumeBillingV1ReadsStartDateOrTime(t *testing.T) {
	// The public v1 spec names the bucket timestamp time; a backend-shaped
	// fixture names it startDate. Either one has to land in Time, and
	// startDate wins when a record carries both.
	for _, tc := range []struct {
		name   string
		fields string
		want   string
	}{
		{"startDate", `"startDate":"2026-06-01T00:00:00Z"`, "2026-06-01T00:00:00Z"},
		{"time", `"time":"2026-06-02T00:00:00Z"`, "2026-06-02T00:00:00Z"},
		{"both", `"startDate":"2026-06-01T00:00:00Z","time":"2026-06-02T00:00:00Z"`, "2026-06-01T00:00:00Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := testServer(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, `[{"amount":0.5,"diskSpaceBilledGb":100,"networkVolumeId":"netvol_abc",`+
					tc.fields+`,"timeBilledMs":86400000}]`)
			})

			records, err := testClient(t, srv.URL).ListNetworkVolumeBillingV1(context.Background(),
				NetworkVolumeBillingQueryV1{BucketSize: "day", NetworkVolumeID: "netvol_abc"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(records) != 1 || records[0].Time == nil || *records[0].Time != tc.want {
				t.Fatalf("records = %+v, want Time %s", records, tc.want)
			}
			if records[0].Amount == nil || *records[0].Amount != 0.5 || records[0].NetworkVolumeID == nil {
				t.Fatalf("records = %+v, want the other fields decoded too", records)
			}
		})
	}
}

func TestListEndpointBillingV1ReportsAFailure(t *testing.T) {
	srv := testServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, "upstream failure")
	})

	_, err := testClient(t, srv.URL).ListEndpointBillingV1(context.Background(), EndpointBillingQueryV1{})
	if err == nil {
		t.Fatal("expected an error for HTTP 502")
	}
	if IsNotFound(err) {
		t.Errorf("error = %v, want a plain status error", err)
	}
}
