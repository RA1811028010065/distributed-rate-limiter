package simplegrpc

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
)

type UnaryHandler func(ctx context.Context, req []byte) ([]byte, error)

type Server struct {
	handlers map[string]UnaryHandler
}

func NewServer() *Server {
	return &Server{handlers: make(map[string]UnaryHandler)}
}

func (s *Server) RegisterUnary(method string, handler UnaryHandler) {
	s.handlers[method] = handler
}

func (s *Server) Serve(l net.Listener) error {
	srv := &http.Server{Handler: s}
	return srv.Serve(l)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
		http.Error(w, "unsupported content type", http.StatusUnsupportedMediaType)
		return
	}
	handler, ok := s.handlers[r.URL.Path]
	if !ok {
		http.Error(w, "unknown method", http.StatusNotFound)
		return
	}
	payload, err := readGRPCFrame(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	respPayload, err := handler(r.Context(), payload)
	if err != nil {
		w.Header().Set("Grpc-Status", "13")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/grpc+proto")
	w.Header().Set("Grpc-Status", "0")
	writeGRPCFrame(w, respPayload)
}

func readGRPCFrame(r io.Reader) ([]byte, error) {
	header := make([]byte, 5)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}
	if header[0] != 0 {
		return nil, errors.New("compressed frames are not supported")
	}
	length := binary.BigEndian.Uint32(header[1:])
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func writeGRPCFrame(w io.Writer, payload []byte) {
	header := make([]byte, 5)
	header[0] = 0
	binary.BigEndian.PutUint32(header[1:], uint32(len(payload)))
	w.Write(header)
	w.Write(payload)
}
