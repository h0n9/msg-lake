package main

import (
	"context"
	"net"
	"testing"

	pb "github.com/h0n9/msg-lake/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/test/bufconn"
)

func TestCheck(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	readiness := health.NewServer()
	healthpb.RegisterHealthServer(server, readiness)
	readiness.SetServingStatus(pb.MsgLake_ServiceDesc.ServiceName, healthpb.HealthCheckResponse_SERVING)
	go server.Serve(listener)
	defer server.Stop()
	// The CLI uses TCP; this test verifies the same service and status contract
	// without opening a socket in restricted test environments.
	option := grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) })
	if err := checkWithOptions(context.Background(), "passthrough:///test", option); err != nil {
		t.Fatal(err)
	}
	readiness.Shutdown()
	if err := checkWithOptions(context.Background(), "passthrough:///test", option); err == nil {
		t.Fatal("NOT_SERVING passed")
	}
}
