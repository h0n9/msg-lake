package agent

import (
	"context"
	"net"
	"os"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"

	"github.com/h0n9/msg-lake/lake"
	pb "github.com/h0n9/msg-lake/proto"
	"github.com/h0n9/msg-lake/util"
)

const (
	DefaultGrpcListenAddr = "0.0.0.0:8080"
)

var Cmd = &cobra.Command{
	Use:   "agent",
	Short: "run msg lake agent",
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := zerolog.New(os.Stdout).With().Timestamp().Str("service", "msg-lake").Logger()
		logLevel, err := zerolog.ParseLevel(util.GetLogLevel())
		if err != nil {
			return err
		}
		zerolog.SetGlobalLevel(logLevel)
		logger.Info().Msg("initalized logger")

		ctx := context.Background()
		logger.Info().Msg("initalized context")

		grpcServer := grpc.NewServer()
		logger.Info().Msg("initalized gRPC server")
		lakeService, err := lake.NewLakeService(ctx, &logger)
		if err != nil {
			return err
		}
		logger.Info().Msg("initalized lake service")

		pb.RegisterLakeServer(grpcServer, lakeService)
		logger.Info().Msg("registered lake service to gRPC server")

		listener, err := net.Listen("tcp", DefaultGrpcListenAddr)
		if err != nil {
			return err
		}
		logger.Info().Msgf("listening gRPC server on %s", DefaultGrpcListenAddr)

		err = grpcServer.Serve(listener)
		if err != nil {
			return err
		}

		return nil
	},
}
