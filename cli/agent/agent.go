package agent

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"

	"github.com/h0n9/msg-lake/lake"
	pb "github.com/h0n9/msg-lake/proto"
	"github.com/h0n9/msg-lake/util"
)

const (
	DefaultGrpcListenAddr = "0.0.0.0:8080"
	DefaultRelayerPortMin = 1024
	DefaultRelayerPortMax = 49151
)

var (
	grpcListenAddr string
	relayerAddrs   []string
	seed           string

	mdnsEnabled bool
	dhtEnabled  bool

	bootstrapPeers []string
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

		ctx, cancel := context.WithCancel(context.Background())
		logger.Info().Msg("initalized context")

		var (
			lakeService *lake.Service = nil
			grpcServer  *grpc.Server  = nil
		)

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		wg := sync.WaitGroup{}
		wg.Add(1)
		go func() {
			s := <-sigCh
			logger.Info().Msgf("got signal %v, attempting graceful shutdown", s)
			cancel()
			if grpcServer != nil {
				grpcServer.GracefulStop()
				logger.Info().Msg("gracefully stopped gRPC server")
			}
			if lakeService != nil {
				lakeService.Close()
				logger.Info().Msg("closed lake service")
			}
			wg.Done()
		}()
		logger.Info().Msg("listening os signal: SIGINT, SIGTERM")

		grpcServer = grpc.NewServer(
			grpc.UnaryInterceptor(util.UnaryServerInterceptor()),
		)
		logger.Info().Msg("initalized gRPC server")
		lakeService, err = lake.NewService(
			ctx,
			&logger,
			[]byte(seed),
			relayerAddrs,
			mdnsEnabled,
			dhtEnabled,
			bootstrapPeers,
		)
		if err != nil {
			return err
		}
		logger.Info().Msg("initalized lake service")

		pb.RegisterLakeServer(grpcServer, lakeService)
		logger.Info().Msg("registered lake service to gRPC server")

		listener, err := net.Listen("tcp", grpcListenAddr)
		if err != nil {
			return err
		}
		logger.Info().Msgf("listening gRPC server on %s", grpcListenAddr)

		err = grpcServer.Serve(listener)
		if err != nil {
			return err
		}

		wg.Wait()

		return nil
	},
}

func init() {
	n, err := rand.Int(
		rand.Reader,
		big.NewInt(DefaultRelayerPortMax-DefaultRelayerPortMin),
	)
	if err != nil {
		panic(err)
	}
	randomRelayerPort := int(n.Int64()) + DefaultRelayerPortMin

	Cmd.Flags().StringVar(&grpcListenAddr, "grpc", DefaultGrpcListenAddr, "gRPC listen address")
	Cmd.Flags().StringSliceVar(&relayerAddrs, "addrs", []string{
		fmt.Sprintf("/ip4/0.0.0.0/udp/%d/quic", randomRelayerPort),
		fmt.Sprintf("/ip6/::/udp/%d/quic", randomRelayerPort),
	}, "relayer port")
	Cmd.Flags().StringVar(&seed, "seed", "", "private key seed")
	Cmd.Flags().BoolVar(&mdnsEnabled, "mdns", false, "enable mdns service")
	Cmd.Flags().BoolVar(&dhtEnabled, "dht", false, "enable kad dht")
	Cmd.Flags().StringSliceVar(&bootstrapPeers, "peers", []string{}, "bootstrap peers for kad dht")
}
