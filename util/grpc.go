package util

import (
	"context"

	"go.uber.org/ratelimit"
	"google.golang.org/grpc"
)

const (
	DefaultUnaryServerInterceptorRateLimit = 10000
)

var (
	unaryServerInterceptorRateLimit int
)

func UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	rl := ratelimit.New(unaryServerInterceptorRateLimit)
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
		rl.Take()
		return handler(ctx, req)
	}
}

func init() {
	tmp, err := getEnvInt("UNARY_SERVER_INTERCEPTOR_RATE_LIMIT", DefaultUnaryServerInterceptorRateLimit)
	if err != nil {
		panic(err)
	}
	unaryServerInterceptorRateLimit = tmp
}
