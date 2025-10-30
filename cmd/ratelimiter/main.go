package main

import (
	"context"
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
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
	logPath := flag.String("log-path", getEnv("LOG_PATH", "logs/runtime.log"), "file to append structured decisions and runtime logs")
	flag.Parse()

	logger, cleanup, err := setupLogger(*logPath)
	if err != nil {
		log.Fatalf("failed to configure logger: %v", err)
	}
	defer cleanup()

	var bus natsutil.Bus
	if *natsURL != "" {
		client, err := natsutil.Connect(*natsURL)
		if err != nil {
			logger.Printf("failed to connect to NATS (%s): %v, falling back to in-memory bus", *natsURL, err)
			bus = natsutil.NewInMemoryBus()
		} else {
			bus = client
			logger.Printf("connected to NATS server at %s", *natsURL)
		}
	} else {
		bus = natsutil.NewInMemoryBus()
		logger.Printf("using in-memory bus; set NATS_URL to enable NATS synchronization")
	}

	limiter := ratelimiter.New(bus, ratelimiter.WithLogger(logger))

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
		logger.Printf("HTTP server listening on %s", *httpAddr)
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
		logger.Printf("gRPC server listening on %s", *grpcAddr)
		if err := grpcSrv.Serve(listener); err != nil {
			errCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		logger.Printf("received signal %s, shutting down", sig)
	case err := <-errCh:
		logger.Printf("server error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	httpSrv.Shutdown(ctx)
	logger.Printf("shutdown complete")
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func setupLogger(logPath string) (*log.Logger, func(), error) {
	outputs := []io.Writer{os.Stdout}
	cleanup := func() {}
	if strings.TrimSpace(logPath) != "" {
		if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
			return nil, cleanup, err
		}
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, cleanup, err
		}
		outputs = append(outputs, f)
		cleanup = func() {
			f.Close()
		}
	}
	writer := io.MultiWriter(outputs...)
	logger := log.New(writer, "", log.LstdFlags|log.Lmicroseconds|log.LUTC)
	return logger, cleanup, nil
}
