package relayer

import (
	"context"
	"crypto/rand"
	"fmt"

	"github.com/libp2p/go-libp2p"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
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
}

func NewRelayer(ctx context.Context, logger *zerolog.Logger, hostname string, port int) (*Relayer, error) {
	subLogger := logger.With().Str("module", "relayer").Logger()

	privKey, pubKey, err := crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, rand.Reader)
	if err != nil {
		return nil, err
	}
	subLogger.Info().Msg("generated key pair for libp2p host")

	h, err := newHost(hostname, port, privKey)
	if err != nil {
		return nil, err
	}
	subLogger.Info().Msg("initialized libp2p host")

	// init mdns service
	dn := newDiscoveryNotifee()
	svc := mdns.NewMdnsService(h, mdnsServiceName, dn)
	err = svc.Start()
	if err != nil {
		return nil, err
	}
	subLogger.Info().Msg("initialized mdns service")

	subLogger.Info().Msgf("listening libp2p host on %v", h.Addrs())

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

		peerChan: dn.peerChan,
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

func newHost(hostname string, port int, privKey crypto.PrivKey) (host.Host, error) {
	ma, err := multiaddr.NewMultiaddr(
		fmt.Sprintf(
			"/ip4/%s/udp/%d/quic",
			hostname,
			port,
		),
	)
	if err != nil {
		return nil, err
	}
	return libp2p.New(
		libp2p.ListenAddrs(ma),
		libp2p.Identity(privKey),
		libp2p.Transport(libp2pquic.NewTransport),
	)
}
