package agent

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	_ "google.golang.org/grpc/encoding/gzip"

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
	RunE:  runAgent,
}

const shutdownTimeout = 10 * time.Second

type serviceResult struct {
	service *lake.Service
	err     error
}

type listenerResult struct {
	listener net.Listener
	err      error
}

func listenWithSignal(signalAt <-chan time.Time, listen func() (net.Listener, error)) (net.Listener, time.Time, bool, error) {
	created := make(chan listenerResult, 1)
	go func() { listener, err := listen(); created <- listenerResult{listener, err} }()
	select {
	case result := <-created:
		return result.listener, time.Time{}, false, result.err
	case at := <-signalAt:
		go func() {
			late := <-created
			if late.listener != nil {
				_ = late.listener.Close()
			}
		}()
		return nil, at, true, nil
	}
}

// awaitInitialization keeps ownership of a constructor's late result after a
// signal. The coordinator can time out while the cleanup goroutine remains
// responsible for any resource that appears later.
func awaitInitialization[T any](cancel context.CancelFunc, signalAt <-chan time.Time, initialized <-chan T, cleanup func(T) error, timeout time.Duration) (T, bool, error) {
	var zero T
	select {
	case result := <-initialized:
		return result, false, nil
	case at := <-signalAt:
		cancel()
		cleanupDone := make(chan error, 1)
		go func() {
			cleanupDone <- cleanup(<-initialized)
		}()
		timer := time.NewTimer(time.Until(at.Add(timeout)))
		defer timer.Stop()
		select {
		case err := <-cleanupDone:
			return zero, true, err
		case <-timer.C:
			return zero, true, fmt.Errorf("shutdown during initialization exceeded %s", timeout)
		}
	}
}

func runAgent(cmd *cobra.Command, args []string) error {
	logger := zerolog.New(os.Stdout).With().Timestamp().Str("service", "msg-lake").Logger()
	level, err := zerolog.ParseLevel(util.GetLogLevel())
	if err != nil {
		return err
	}
	zerolog.SetGlobalLevel(level)
	initCtx, initCancel := context.WithCancel(context.Background())
	defer initCancel()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	signalAt := make(chan time.Time, 1)
	stopSignals := make(chan struct{})
	defer close(stopSignals)
	go func() {
		select {
		case <-signals:
			signalAt <- time.Now()
		case <-stopSignals:
		}
	}()
	initialized := make(chan serviceResult, 1)
	go func() {
		service, err := lake.NewService(initCtx, &logger, []byte(seed), relayerAddrs, mdnsEnabled, dhtEnabled, bootstrapPeers)
		initialized <- serviceResult{service, err}
	}()
	result, aborted, err := awaitInitialization(initCancel, signalAt, initialized, func(late serviceResult) error {
		if late.service != nil {
			return late.service.Close()
		}
		return late.err
	}, shutdownTimeout)
	if aborted {
		return err
	}
	if result.err != nil {
		return result.err
	}
	service := result.service
	server := grpc.NewServer(grpc.UnaryInterceptor(util.UnaryServerInterceptor()))
	pb.RegisterMsgLakeServer(server, service)
	listener, listenSignalAt, interrupted, err := listenWithSignal(signalAt, func() (net.Listener, error) { return net.Listen("tcp", grpcListenAddr) })
	if interrupted {
		return shutdownService(service, server, listenSignalAt)
	}
	if err != nil {
		return errors.Join(err, shutdownService(service, server, shutdownStart(signalAt)))
	}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	select {
	case at := <-signalAt:
		return shutdownService(service, server, at)
	case err := <-served:
		return errors.Join(err, shutdownService(service, server, shutdownStart(signalAt)))
	}
}

func shutdownStart(signalAt <-chan time.Time) time.Time {
	select {
	case at := <-signalAt:
		return at
	default:
		return time.Now()
	}
}

type shutdownTarget interface {
	BeginShutdown()
	Close() error
	CancelBackend()
}

type grpcLifecycle interface {
	GracefulStop()
	Stop()
}

func shutdownService(service shutdownTarget, server grpcLifecycle, at time.Time) error {
	return shutdownServiceWithin(service, server, at, shutdownTimeout)
}

func shutdownServiceWithin(service shutdownTarget, server grpcLifecycle, at time.Time, timeout time.Duration) error {
	deadline := at.Add(timeout)
	service.BeginShutdown()
	grpcDone := make(chan struct{})
	go func() { server.GracefulStop(); close(grpcDone) }()
	cleanupDone := make(chan error, 1)
	go func() { cleanupDone <- service.Close() }()
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	var cleanupErr error
	for grpcDone != nil || cleanupDone != nil {
		select {
		case <-grpcDone:
			grpcDone = nil
		case cleanupErr = <-cleanupDone:
			cleanupDone = nil
		case <-timer.C:
			service.CancelBackend()
			go server.Stop()
			return fmt.Errorf("shutdown exceeded %s", timeout)
		}
	}
	if time.Until(deadline) <= 0 {
		service.CancelBackend()
		go server.Stop()
		return fmt.Errorf("shutdown exceeded %s", timeout)
	}
	return cleanupErr
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
		fmt.Sprintf("/ip4/0.0.0.0/udp/%d/quic-v1", randomRelayerPort),
		fmt.Sprintf("/ip6/::/udp/%d/quic-v1", randomRelayerPort),
	}, "relayer port")
	Cmd.Flags().StringVar(&seed, "seed", "", "private key seed")
	Cmd.Flags().BoolVar(&mdnsEnabled, "mdns", false, "enable mdns service")
	Cmd.Flags().BoolVar(&dhtEnabled, "dht", false, "enable kad dht")
	Cmd.Flags().StringSliceVar(&bootstrapPeers, "peers", []string{}, "bootstrap peers for kad dht")
}
