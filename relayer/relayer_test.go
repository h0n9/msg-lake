package relayer

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/rs/zerolog"
)

func TestDiscoveryDisabledAndCanceledExit(t *testing.T) {
	logger := zerolog.Nop()
	disabled := &Relayer{logger: &logger}
	if err := disabled.DiscoverPeers(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	waiting := &Relayer{ctx: ctx, logger: &logger, peerChan: make(chan peer.AddrInfo)}
	done := make(chan error, 1)
	go func() { done <- waiting.DiscoverPeers() }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("discovery did not exit after cancellation")
	}
}

func TestDiscoveryCallbackDoesNotBlockWhenQueueFullOrCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	notifee := newDiscoveryNotifee(ctx)
	for i := 0; i < cap(notifee.peerChan); i++ {
		notifee.HandlePeerFound(peer.AddrInfo{})
	}
	done := make(chan struct{})
	go func() { notifee.HandlePeerFound(peer.AddrInfo{}); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("full discovery queue blocked callback")
	}
	cancel()
	done = make(chan struct{})
	go func() { notifee.HandlePeerFound(peer.AddrInfo{}); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("canceled discovery blocked callback")
	}
}
