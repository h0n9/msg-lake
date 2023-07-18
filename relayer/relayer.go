package relayer

import (
	"context"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
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
	protocolID      = protocol.ID("/msg-lake/v1.0-beta-0")
	mdnsServiceName = "_p2p_msg-lake._udp"
)

type Relayer struct {
	ctx    context.Context
	logger *zerolog.Logger

	privKey *crypto.PrivKey
	pubKey  *crypto.PubKey

	h         host.Host
	msgCenter *msg.Center

	mdnsSvc  mdns.Service
	peerChan <-chan peer.AddrInfo

	d *dht.IpfsDHT
}

func NewRelayer(ctx context.Context, logger *zerolog.Logger, seed []byte, addrs []string, mdnsEnabled bool, dhtEnabled bool, bootstrapPeers []string) (*Relayer, error) {
	subLogger := logger.With().Str("module", "relayer").Logger()

	var (
		privKey *crypto.PrivKey
		err     error
	)
	privKey, err = crypto.GenPrivKey()
	if err != nil {
		return nil, err
	}
	if seed != nil && len(seed) > 0 {
		privKey, err = crypto.GenPrivKeyFromSeed(seed)
		if err != nil {
			return nil, err
		}
	}
	subLogger.Info().Msg("generated key pair for libp2p host")

	h, err := newHost(addrs, privKey)
	if err != nil {
		return nil, err
	}
	subLogger.Info().Msg("initialized libp2p host")

	// convert string formatted bootstrap peer addrs to peer.AddrInfo
	pis := []peer.AddrInfo{}
	for _, addr := range bootstrapPeers {
		pi, err := peer.AddrInfoFromString(addr)
		if err != nil {
			logger.Err(err).Str("addr", addr).Msg("")
			continue
		}
		pis = append(pis, *pi)
	}

	relayer := Relayer{
		ctx:    ctx,
		logger: &subLogger,

		privKey: privKey,
		pubKey:  privKey.PubKey(),

		h: h,
	}

	// init mdns service
	if mdnsEnabled {
		dn := newDiscoveryNotifee()
		svc := mdns.NewMdnsService(h, mdnsServiceName, dn)
		err = svc.Start()
		if err != nil {
			return nil, err
		}
		relayer.mdnsSvc = svc
		relayer.peerChan = dn.peerChan
		subLogger.Info().Msg("initialized mdns service")
	}

	// init kad dht
	if dhtEnabled {
		d, err := dht.New(
			ctx,
			h,
			dht.BootstrapPeers(pis...),
			dht.Mode(dht.ModeServer),
		)
		if err != nil {
			return nil, err
		}
		d.RoutingTable().PeerAdded = func(pi peer.ID) {
			subLogger.Info().Str("peer", pi.String()).Msg("connected")
		}
		d.RoutingTable().PeerRemoved = func(pi peer.ID) {
			subLogger.Info().Str("peer", pi.String()).Msg("disconnected")
		}
		relayer.d = d
		subLogger.Info().Msg("initialized libp2p kad dht")

		d.Bootstrap(ctx)
		subLogger.Info().Msg("bootstrapped libp2p kad dht")
	}

	subLogger.Info().Msgf("listening address %v", h.Addrs())
	subLogger.Info().Msgf("libp2p peer ID %s", h.ID())

	// TODO: make this options customizable with external config file
	ps, err := pubsub.NewGossipSub(
		ctx,
		h,
		// msgs routing internally don't need to be signed and verified
		pubsub.WithMessageSigning(false),
		// msgs are removed from time cache after 3 seconds since first seen
		pubsub.WithSeenMessagesTTL(3*time.Second),
		pubsub.WithSeenMessagesStrategy(timecache.Strategy_FirstSeen),
	)
	if err != nil {
		return nil, err
	}
	relayer.msgCenter = msg.NewCenter(ctx, &subLogger, ps)
	subLogger.Info().Msg("initialized gossip sub")

	return &relayer, nil
}

func (relayer *Relayer) Close() {
	if relayer.mdnsSvc != nil {
		err := relayer.mdnsSvc.Close()
		if err != nil {
			relayer.logger.Err(err).Msg("")
		}
		relayer.logger.Info().Msg("closed mdns service")
	}
	if relayer.d != nil {
		err := relayer.d.Close()
		if err != nil {
			relayer.logger.Err(err).Msg("")
		}
		relayer.logger.Info().Msg("closed kad dht")
	}
	err := relayer.h.Close()
	if err != nil {
		relayer.logger.Err(err).Msg("")
	}
	relayer.logger.Info().Msg("closed relayer")
}

func (relayer *Relayer) DiscoverPeers() error {
	for {
		relayer.logger.Info().Msg("waiting peers")
		peer := <-relayer.peerChan // blocks until discover new peers
		relayer.logger.Info().Str("peer", peer.String()).Msg("found")

		relayer.logger.Info().Str("peer", peer.String()).Msg("connecting")
		err := relayer.h.Connect(relayer.ctx, peer)
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
}

func newDiscoveryNotifee() *discoveryNotifee {
	return &discoveryNotifee{
		peerChan: make(chan peer.AddrInfo),
	}
}

// interface to be called when new  peer is found
func (n *discoveryNotifee) HandlePeerFound(pi peer.AddrInfo) {
	n.peerChan <- pi
}

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
