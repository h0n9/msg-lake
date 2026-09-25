package relayer

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p-pubsub/timecache"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	libp2pquic "github.com/libp2p/go-libp2p/p2p/transport/quic"
	"github.com/postie-labs/go-postie-lib/crypto"

	"github.com/multiformats/go-multiaddr"
	"github.com/rs/zerolog"

	"github.com/h0n9/msg-lake/msg"
)

const (
	protocolID      = protocol.ID("/msg-lake/v1.0-beta-1")
	mdnsServiceName = "_p2p_msg-lake._udp"
)

type Relayer struct {
	ctx       context.Context
	cancel    context.CancelFunc
	closeOnce sync.Once
	closeErr  error
	logger    *zerolog.Logger

	privKey *crypto.PrivKey
	pubKey  *crypto.PubKey

	host      host.Host
	msgCenter *msg.Center

	mdnsSvc  mdns.Service
	peerChan <-chan peer.AddrInfo

	kadDHT *dht.IpfsDHT
}

func NewRelayer(ctx context.Context, logger *zerolog.Logger, seed []byte, addrs []string, mdnsEnabled bool, dhtEnabled bool, bootstrapPeers []string) (*Relayer, error) {
	subLogger := logger.With().Str("module", "relayer").Logger()
	backendCtx, backendCancel := context.WithCancel(ctx)

	var (
		privKey *crypto.PrivKey
		err     error
	)
	privKey, err = crypto.GenPrivKey()
	if err != nil {
		backendCancel()
		return nil, err
	}
	if len(seed) > 0 {
		privKey, err = crypto.GenPrivKeyFromSeed(seed)
		if err != nil {
			backendCancel()
			return nil, err
		}
	}
	subLogger.Info().Msg("generated key pair for libp2p host")

	p2pHost, err := newHost(addrs, privKey)
	if err != nil {
		backendCancel()
		return nil, err
	}
	subLogger.Info().Msg("initialized libp2p host")

	// convert string formatted bootstrap peer addrs to peer.AddrInfo
	bootstrapPeerInfos := []peer.AddrInfo{}
	for _, addr := range bootstrapPeers {
		peerInfo, err := peer.AddrInfoFromString(addr)
		if err != nil {
			logger.Err(err).Str("addr", addr).Msg("")
			continue
		}
		bootstrapPeerInfos = append(bootstrapPeerInfos, *peerInfo)
	}

	relayer := Relayer{
		ctx:    backendCtx,
		cancel: backendCancel,
		logger: &subLogger,

		privKey: privKey,
		pubKey:  privKey.PubKey(),

		host: p2pHost,
	}
	initialized := false
	defer func() {
		if !initialized {
			relayer.Close()
		}
	}()

	// init mdns service
	if mdnsEnabled {
		discoveryNotifee := newDiscoveryNotifee(backendCtx)
		mdnsService := mdns.NewMdnsService(p2pHost, mdnsServiceName, discoveryNotifee)
		relayer.mdnsSvc = mdnsService
		err = mdnsService.Start()
		if err != nil {
			return nil, err
		}
		relayer.peerChan = discoveryNotifee.peerChan
		subLogger.Info().Msg("initialized mdns service")
	}

	// init kad dht
	if dhtEnabled {
		kadDHT, err := dht.New(
			backendCtx,
			p2pHost,
			dht.BootstrapPeers(bootstrapPeerInfos...),
			dht.Mode(dht.ModeServer),
		)
		if err != nil {
			return nil, err
		}
		kadDHT.RoutingTable().PeerAdded = func(peerID peer.ID) {
			subLogger.Info().Str("peer", peerID.String()).Msg("connected")
		}
		kadDHT.RoutingTable().PeerRemoved = func(peerID peer.ID) {
			subLogger.Info().Str("peer", peerID.String()).Msg("disconnected")
		}
		relayer.kadDHT = kadDHT
		subLogger.Info().Msg("initialized libp2p kad dht")

		if err := kadDHT.Bootstrap(backendCtx); err != nil {
			return nil, err
		}
		subLogger.Info().Msg("bootstrapped libp2p kad dht")
	}

	subLogger.Info().Msgf("listening address %v", p2pHost.Addrs())
	subLogger.Info().Msgf("libp2p peer ID %s", p2pHost.ID())

	// TODO: make this options customizable with external config file
	ps, err := pubsub.NewGossipSub(
		backendCtx,
		p2pHost,
		pubsub.WithGossipSubProtocols(
			[]protocol.ID{protocolID},
			func(_ pubsub.GossipSubFeature, id protocol.ID) bool {
				return id == protocolID
			},
		),
		// Disable GossipSub transport signatures. Topic validators verify the
		// end-to-end client signature carried by each capsule instead.
		pubsub.WithMessageSigning(false),
		// msgs are removed from time cache after 3 seconds since first seen
		pubsub.WithSeenMessagesTTL(3*time.Second),
		pubsub.WithSeenMessagesStrategy(timecache.Strategy_FirstSeen),
	)
	if err != nil {
		return nil, err
	}
	relayer.msgCenter = msg.NewCenter(backendCtx, &subLogger, ps)
	subLogger.Info().Str("protocol-id", string(protocolID)).Msg("initialized gossip sub")

	initialized = true
	return &relayer, nil
}

func (relayer *Relayer) Close() error {
	relayer.closeOnce.Do(relayer.close)
	return relayer.closeErr
}

func (relayer *Relayer) close() {
	if relayer.msgCenter != nil {
		if err := relayer.msgCenter.Close(); err != nil {
			relayer.closeErr = errors.Join(relayer.closeErr, err)
			relayer.logger.Err(err).Msg("closing msg center")
		}
	}
	relayer.cancel()
	if relayer.mdnsSvc != nil {
		err := relayer.mdnsSvc.Close()
		if err != nil {
			relayer.closeErr = errors.Join(relayer.closeErr, err)
			relayer.logger.Err(err).Msg("")
		}
		relayer.logger.Info().Msg("closed mdns service")
	}
	if relayer.kadDHT != nil {
		err := relayer.kadDHT.Close()
		if err != nil {
			relayer.closeErr = errors.Join(relayer.closeErr, err)
			relayer.logger.Err(err).Msg("")
		}
		relayer.logger.Info().Msg("closed kad dht")
	}
	err := relayer.host.Close()
	if err != nil {
		relayer.closeErr = errors.Join(relayer.closeErr, err)
		relayer.logger.Err(err).Msg("")
	}
	relayer.logger.Info().Msg("closed relayer")
}

func (relayer *Relayer) DiscoverPeers() error {
	if relayer.peerChan == nil {
		return nil
	}
	for {
		relayer.logger.Info().Msg("waiting peers")
		var peer peer.AddrInfo
		select {
		case <-relayer.ctx.Done():
			return nil
		case peer = <-relayer.peerChan:
		}
		relayer.logger.Info().Str("peer", peer.String()).Msg("found")

		relayer.logger.Info().Str("peer", peer.String()).Msg("connecting")
		err := relayer.host.Connect(relayer.ctx, peer)
		if err != nil {
			relayer.logger.Err(err).Str("peer", peer.String()).Msg("")
			continue
		}
		relayer.logger.Info().Str("peer", peer.String()).Msg("connected")
	}
}

func (relayer *Relayer) GetMsgCenter() *msg.Center {
	return relayer.msgCenter
}

type discoveryNotifee struct {
	peerChan chan peer.AddrInfo
	ctx      context.Context
}

func newDiscoveryNotifee(ctx context.Context) *discoveryNotifee {
	return &discoveryNotifee{
		peerChan: make(chan peer.AddrInfo, 64),
		ctx:      ctx,
	}
}

// interface to be called when new  peer is found
func (n *discoveryNotifee) HandlePeerFound(peerInfo peer.AddrInfo) {
	select {
	case n.peerChan <- peerInfo:
	case <-n.ctx.Done():
	default: // Discovery hints may be dropped when Connect cannot keep up.
	}
}

func (relayer *Relayer) CancelBackend() { relayer.cancel() }

func newHost(addrs []string, privKey *crypto.PrivKey) (host.Host, error) {
	listenAddrs, err := generateListenAddrs(addrs)
	if err != nil {
		return nil, err
	}
	privKeyP2P, err := privKey.ToECDSAP2P()
	if err != nil {
		return nil, err
	}
	return libp2p.New(
		libp2p.ListenAddrs(listenAddrs...),
		libp2p.Identity(privKeyP2P),
		libp2p.Transport(libp2pquic.NewTransport),
	)
}

func generateListenAddrs(addrs []string) ([]multiaddr.Multiaddr, error) {
	listenAddrs := make([]multiaddr.Multiaddr, 0, len(addrs))
	for _, s := range addrs {
		addr, err := multiaddr.NewMultiaddr(s)
		if err != nil {
			return nil, err
		}
		listenAddrs = append(listenAddrs, addr)
	}
	return listenAddrs, nil
}
