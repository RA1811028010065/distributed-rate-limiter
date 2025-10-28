package pbcodec

import (
	"reflect"
	"testing"
)

func TestAllowRequestRoundTrip(t *testing.T) {
	req := &AllowRequest{Key: "user:1", Tokens: 2, MaxTokens: 5, RefillRate: 1}
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
	res := &AllowResponse{Allowed: true, RemainingTokens: 3, Message: "ok"}
	data := MarshalAllowResponse(res)
	var decoded AllowResponse
	if err := UnmarshalAllowResponse(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if !reflect.DeepEqual(res, &decoded) {
		t.Fatalf("round trip mismatch: %+v != %+v", res, &decoded)
	}
}
