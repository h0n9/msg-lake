package relayer

import (
	"context"
	"crypto/rand"
	"fmt"
	mrand "math/rand"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	libp2pquic "github.com/libp2p/go-libp2p/p2p/transport/quic"

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

	privKey crypto.PrivKey
	pubKey  crypto.PubKey

	h         host.Host
	msgCenter *msg.Center

	peerChan <-chan peer.AddrInfo

	d *dht.IpfsDHT
}

func NewRelayer(ctx context.Context, logger *zerolog.Logger, seed int64, port int, bootstrapPeers []string) (*Relayer, error) {
	subLogger := logger.With().Str("module", "relayer").Logger()

	keyPairSrc := rand.Reader
	if seed != 0 {
		keyPairSrc = mrand.New(mrand.NewSource(seed))
	}
	privKey, pubKey, err := crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, keyPairSrc)
	if err != nil {
		return nil, err
	}
	subLogger.Info().Msg("generated key pair for libp2p host")

	h, err := newHost(port, privKey)
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
		subLogger.Info().Str("peer", pi.String()).Msg("connecting")
		err = h.Connect(ctx, *pi)
		if err != nil {
			logger.Err(err).Str("peer", pi.String()).Msg("")
			continue
		}
		subLogger.Info().Str("peer", pi.String()).Msg("connected")
		pis = append(pis, *pi)
	}

	// init kad dht
	d, err := dht.New(
		ctx,
		h,
		dht.BootstrapPeers(pis...),
		dht.Mode(dht.ModeServer),
	)
	if err != nil {
		return nil, err
	}
	subLogger.Info().Msg("initialized libp2p kad dht")

	d.Bootstrap(ctx)
	subLogger.Info().Msg("bootstrapped libp2p kad dht")

	// init mdns service
	// dn := newDiscoveryNotifee()
	// svc := mdns.NewMdnsService(h, mdnsServiceName, dn)
	// err = svc.Start()
	// if err != nil {
	// 	return nil, err
	// }
	// subLogger.Info().Msg("initialized mdns service")

	subLogger.Info().Msgf("listening address %v", h.Addrs())
	subLogger.Info().Msgf("libp2p peer ID %s", h.ID())

	ps, err := pubsub.NewGossipSub(ctx, h)
	if err != nil {
		return nil, err
	}
	subLogger.Info().Msg("initialized gossip sub")

	return &Relayer{
		ctx:    ctx,
		logger: &subLogger,

		privKey: privKey,
		pubKey:  pubKey,

		h:         h,
		msgCenter: msg.NewCenter(ctx, &subLogger, ps),

		d: d,
		// peerChan: dn.peerChan,
	}, nil
}

func (relayer *Relayer) Close() {
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

func newHost(port int, privKey crypto.PrivKey) (host.Host, error) {
	listenAddrs, err := generateListenAddrs(port)
	if err != nil {
		return nil, err
	}
	return libp2p.New(
		libp2p.ListenAddrs(listenAddrs...),
		libp2p.Identity(privKey),
		libp2p.Transport(libp2pquic.NewTransport),
	)
}

func generateListenAddrs(port int) ([]multiaddr.Multiaddr, error) {
	addrs := []string{
		"/ip4/0.0.0.0/udp/%d/quic",
		"/ip6/::/udp/%d/quic",
	}
	listenAddrs := make([]multiaddr.Multiaddr, 0, len(addrs))
	for _, s := range addrs {
		addr, err := multiaddr.NewMultiaddr(fmt.Sprintf(s, port))
		if err != nil {
			return nil, err
		}
		listenAddrs = append(listenAddrs, addr)
	}
	return listenAddrs, nil
}
