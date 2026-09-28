package agent

import (
	"context"
	"net"
	"testing"

	pb "github.com/h0n9/msg-lake/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/test/bufconn"
)

func TestAgentGRPCHealth(t *testing.T) {
	server, readiness := newGRPCServer(&pb.UnimplementedMsgLakeServer{})
	listener := bufconn.Listen(1024 * 1024)
	go server.Serve(listener)
	defer server.Stop()
	conn, err := grpc.NewClient("passthrough:///agent", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	check := func() healthpb.HealthCheckResponse_ServingStatus {
		res, err := healthpb.NewHealthClient(conn).Check(context.Background(), &healthpb.HealthCheckRequest{Service: pb.MsgLake_ServiceDesc.ServiceName})
		if err != nil {
			t.Fatal(err)
		}
		return res.GetStatus()
	}
	if got := check(); got != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("initial status: %s", got)
	}
	readiness.Shutdown()
	if got := check(); got != healthpb.HealthCheckResponse_NOT_SERVING {
		t.Fatalf("shutdown status: %s", got)
	}
}
