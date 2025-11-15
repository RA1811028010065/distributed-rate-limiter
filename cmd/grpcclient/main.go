package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/example/distributed-rate-limiter/internal/pbcodec"
)

func main() {
	log.SetFlags(0)
	addr := flag.String("addr", "http://localhost:8081", "gRPC base address (scheme://host:port)")
	key := flag.String("key", "grpc-smoke", "rate limit key")
	tokens := flag.Int64("tokens", 1, "tokens to consume")
	maxTokens := flag.Int64("max-tokens", 5, "bucket capacity")
	refillRate := flag.Int64("refill-rate", 5, "tokens added per second")
	source := flag.String("source", "grpc-client", "caller source identifier")
	timeout := flag.Duration("timeout", 5*time.Second, "request timeout")
	pretty := flag.Bool("pretty", true, "print formatted JSON response")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	req := &pbcodec.AllowRequest{
		Key:        strings.TrimSpace(*key),
		Tokens:     *tokens,
		MaxTokens:  *maxTokens,
		RefillRate: *refillRate,
		Source:     strings.TrimSpace(*source),
	}
	payload := pbcodec.MarshalAllowRequest(req)
	frame := encodeFrame(payload)

	endpoint := strings.TrimSuffix(*addr, "/") + "/ratelimiter.v1.RateLimiter/Allow"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(frame))
	if err != nil {
		log.Fatalf("build request: %v", err)
	}
	httpReq.Header.Set("Content-Type", "application/grpc+proto")
	httpReq.Header.Set("TE", "trailers")
	httpReq.Header.Set("User-Agent", "rate-limiter-grpc-client/1.0")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		log.Fatalf("perform request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		log.Fatalf("unexpected status %s: %s", resp.Status, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("read response: %v", err)
	}
	msg, err := decodeFrame(body)
	if err != nil {
		log.Fatalf("decode frame: %v", err)
	}
	var allow pbcodec.AllowResponse
	if err := pbcodec.UnmarshalAllowResponse(msg, &allow); err != nil {
		log.Fatalf("decode payload: %v", err)
	}

	if *pretty {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(allow); err != nil {
			log.Fatalf("encode JSON: %v", err)
		}
		return
	}
	fmt.Printf("%+v\n", allow)
}

func encodeFrame(payload []byte) []byte {
	frame := make([]byte, 5+len(payload))
	frame[0] = 0
	binary.BigEndian.PutUint32(frame[1:], uint32(len(payload)))
	copy(frame[5:], payload)
	return frame
}

func decodeFrame(body []byte) ([]byte, error) {
	if len(body) < 5 {
		return nil, errors.New("response shorter than gRPC frame header")
	}
	if body[0] != 0 {
		return nil, errors.New("compressed responses are not supported")
	}
	length := binary.BigEndian.Uint32(body[1:5])
	if int(length) > len(body)-5 {
		return nil, errors.New("declared response length exceeds payload")
	}
	return body[5 : 5+length], nil
}
