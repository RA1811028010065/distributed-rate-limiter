package main

import (
	"context"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/distributed-rate-limiter/internal/ratelimiter"
	"github.com/example/distributed-rate-limiter/internal/server"
	"github.com/example/distributed-rate-limiter/pkg/natsutil"
	"github.com/example/distributed-rate-limiter/pkg/simplegrpc"
)

func main() {
	httpAddr := flag.String("http", getEnv("HTTP_ADDR", ":8080"), "HTTP listen address")
	grpcAddr := flag.String("grpc", getEnv("GRPC_ADDR", ":8081"), "gRPC listen address")
	natsURL := flag.String("nats", os.Getenv("NATS_URL"), "NATS connection URL")
	flag.Parse()

	var bus natsutil.Bus
	if *natsURL != "" {
		client, err := natsutil.Connect(*natsURL)
		if err != nil {
			log.Printf("failed to connect to NATS (%s): %v, falling back to in-memory bus", *natsURL, err)
			bus = natsutil.NewInMemoryBus()
		} else {
			bus = client
			log.Printf("connected to NATS server at %s", *natsURL)
		}
	} else {
		bus = natsutil.NewInMemoryBus()
		log.Printf("using in-memory bus; set NATS_URL to enable NATS synchronization")
	}

	limiter := ratelimiter.New(bus)

	httpSrv := &http.Server{
		Addr:    *httpAddr,
		Handler: server.NewHTTPServer(limiter).Handler(),
	}

	grpcSrv := simplegrpc.NewServer()
	grpcSrv.RegisterUnary("/ratelimiter.v1.RateLimiter/Allow", func(ctx context.Context, payload []byte) ([]byte, error) {
		return limiter.AllowFromBytes(ctx, payload)
	})

	errCh := make(chan error, 2)

	go func() {
		log.Printf("HTTP server listening on %s", *httpAddr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	go func() {
		listener, err := net.Listen("tcp", *grpcAddr)
		if err != nil {
			errCh <- err
			return
		}
		log.Printf("gRPC server listening on %s", *grpcAddr)
		if err := grpcSrv.Serve(listener); err != nil {
			errCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Printf("received signal %s, shutting down", sig)
	case err := <-errCh:
		log.Printf("server error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	httpSrv.Shutdown(ctx)
	log.Printf("shutdown complete")
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
