package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	pb "github.com/h0n9/msg-lake/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func check(ctx context.Context, target string) error {
	return checkWithOptions(ctx, target)
}

func checkWithOptions(ctx context.Context, target string, options ...grpc.DialOption) error {
	conn, err := grpc.NewClient(target, append([]grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}, options...)...)
	if err != nil {
		return err
	}
	defer conn.Close()
	response, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{Service: pb.MsgLake_ServiceDesc.ServiceName})
	if err != nil {
		return err
	}
	if response.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		return fmt.Errorf("MsgLake health: %s", response.GetStatus())
	}
	return nil
}

func main() {
	target := flag.String("target", "localhost:8080", "gRPC health target")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := check(ctx, *target); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
