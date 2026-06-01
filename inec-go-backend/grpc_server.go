package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
)

// GRPCServer wraps the gRPC server and its services.
type GRPCServer struct {
	server *grpc.Server
	port   string
}

// NewGRPCServer creates a configured gRPC server with keepalive and interceptors.
func NewGRPCServer() *GRPCServer {
	port := os.Getenv("GRPC_PORT")
	if port == "" {
		port = "50051"
	}

	opts := []grpc.ServerOption{
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle:     5 * time.Minute,
			MaxConnectionAge:      30 * time.Minute,
			MaxConnectionAgeGrace: 10 * time.Second,
			Time:                  1 * time.Minute,
			Timeout:               20 * time.Second,
		}),
		grpc.UnaryInterceptor(grpcUnaryInterceptor),
		grpc.StreamInterceptor(grpcStreamInterceptor),
		grpc.MaxRecvMsgSize(10 * 1024 * 1024), // 10MB
	}

	s := grpc.NewServer(opts...)

	// Register health check service
	healthServer := health.NewServer()
	healthpb.RegisterHealthServer(s, healthServer)
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	// Enable reflection for development tooling (grpcurl, grpc-cli)
	reflection.Register(s)

	return &GRPCServer{server: s, port: port}
}

// Start begins listening on the configured port.
func (g *GRPCServer) Start() error {
	lis, err := net.Listen("tcp", ":"+g.port)
	if err != nil {
		return fmt.Errorf("grpc listen: %w", err)
	}
	log.Info().Str("port", g.port).Msg("gRPC server starting")
	return g.server.Serve(lis)
}

// GracefulStop drains active RPCs before stopping.
func (g *GRPCServer) GracefulStop() {
	log.Info().Msg("gRPC server shutting down gracefully")
	g.server.GracefulStop()
}

// Unary interceptor for logging, metrics, and auth.
func grpcUnaryInterceptor(
	ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (interface{}, error) {
	start := time.Now()
	resp, err := handler(ctx, req)
	duration := time.Since(start)

	logger := log.With().
		Str("method", info.FullMethod).
		Dur("duration", duration).
		Logger()

	if err != nil {
		logger.Error().Err(err).Msg("gRPC unary call failed")
	} else {
		logger.Debug().Msg("gRPC unary call")
	}

	return resp, err
}

// Stream interceptor for logging.
func grpcStreamInterceptor(
	srv interface{},
	ss grpc.ServerStream,
	info *grpc.StreamServerInfo,
	handler grpc.StreamHandler,
) error {
	start := time.Now()
	err := handler(srv, ss)
	duration := time.Since(start)

	logger := log.With().
		Str("method", info.FullMethod).
		Dur("duration", duration).
		Logger()

	if err != nil {
		logger.Error().Err(err).Msg("gRPC stream failed")
	} else {
		logger.Debug().Msg("gRPC stream completed")
	}

	return err
}
