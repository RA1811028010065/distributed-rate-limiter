package pbcodec

import (
	"reflect"
	"testing"
)

func TestAllowRequestRoundTrip(t *testing.T) {
	req := &AllowRequest{Key: "user:1", Tokens: 2, MaxTokens: 5, RefillRate: 1, Source: "10.0.0.5"}
	data := MarshalAllowRequest(req)
	var decoded AllowRequest
	if err := UnmarshalAllowRequest(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if !reflect.DeepEqual(req, &decoded) {
		t.Fatalf("round trip mismatch: %+v != %+v", req, &decoded)
	}
}

func TestAllowResponseRoundTrip(t *testing.T) {
	res := &AllowResponse{
		Allowed:         true,
		RemainingTokens: 3,
		Message:         "ok",
		AllowedHits:     9,
		DeniedHits:      2,
		LastAllowedAt:   "2025-10-30T00:00:00Z",
		LastDeniedAt:    "2025-10-30T00:01:00Z",
		Algorithm:       "token_bucket",
		StrategyReason:  "default",
	}
	data := MarshalAllowResponse(res)
	var decoded AllowResponse
	if err := UnmarshalAllowResponse(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if !reflect.DeepEqual(res, &decoded) {
		t.Fatalf("round trip mismatch: %+v != %+v", res, &decoded)
	}
}
