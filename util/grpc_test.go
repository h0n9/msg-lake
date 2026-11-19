package util

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func TestHealthCheckBypassesPublishRateLimit(t *testing.T) {
	old := unaryServerInterceptorRateLimit
	unaryServerInterceptorRateLimit = 1
	defer func() { unaryServerInterceptorRateLimit = old }()
	intercept := UnaryServerInterceptor()
	handler := func(context.Context, interface{}) (interface{}, error) { return nil, nil }
	if _, err := intercept(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/MsgLake/Publish"}, handler); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	for range 10 {
		if _, err := intercept(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: healthpb.Health_Check_FullMethodName}, handler); err != nil {
			t.Fatal(err)
		}
	}
	if time.Since(started) > 200*time.Millisecond {
		t.Fatal("health checks waited behind publish rate limit")
	}
}
