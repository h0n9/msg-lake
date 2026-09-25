package agent

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type blockedLifecycle struct {
	publishes chan struct{}
	canceled  atomic.Bool
	closed    atomic.Bool
	closeErr  error
}

func (s *blockedLifecycle) BeginShutdown() {}
func (s *blockedLifecycle) Close() error   { <-s.publishes; s.closed.Store(true); return s.closeErr }
func (s *blockedLifecycle) CancelBackend() { s.canceled.Store(true) }

type blockedGRPC struct {
	stopCalled      chan bool
	stopped         chan struct{}
	backendCanceled *atomic.Bool
}

func (g *blockedGRPC) GracefulStop() { <-g.stopped }
func (g *blockedGRPC) Stop()         { g.stopCalled <- g.backendCanceled.Load(); close(g.stopped) }

func TestShutdownWatchdogCancelsBackendBeforeStop(t *testing.T) {
	service := &blockedLifecycle{publishes: make(chan struct{})}
	server := &blockedGRPC{stopCalled: make(chan bool, 1), stopped: make(chan struct{}), backendCanceled: &service.canceled}
	returned := make(chan error, 1)
	go func() { returned <- shutdownServiceWithin(service, server, time.Now(), 20*time.Millisecond) }()
	select {
	case err := <-returned:
		if err == nil || !strings.Contains(err.Error(), "shutdown exceeded") {
			t.Fatalf("result = %v, want timeout", err)
		}
	case <-time.After(time.Second):
		t.Fatal("watchdog did not return")
	}
	select {
	case canceled := <-server.stopCalled:
		if !canceled {
			t.Fatal("grpc.Stop called before backend cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("grpc.Stop not called")
	}
	if service.closed.Load() {
		t.Fatal("backend close completed before blocked Publish drained")
	}
	close(service.publishes)
}

func TestShutdownReturnsBackendCleanupError(t *testing.T) {
	want := errors.New("topic close failed")
	service := &blockedLifecycle{publishes: make(chan struct{}), closeErr: want}
	close(service.publishes)
	server := &blockedGRPC{stopped: make(chan struct{}), stopCalled: make(chan bool, 1), backendCanceled: &service.canceled}
	close(server.stopped)
	if err := shutdownServiceWithin(service, server, time.Now(), time.Second); !errors.Is(err, want) {
		t.Fatalf("shutdown error = %v, want %v", err, want)
	}
	if service.canceled.Load() {
		t.Fatal("normal cleanup canceled backend before Box teardown")
	}
}

func TestSignalDuringInitializationCancelsAndCleansLateResource(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	initialized := make(chan int, 1)
	signalAt := make(chan time.Time, 1)
	signalAt <- time.Now()
	cleaned := make(chan int, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		result, aborted, err := awaitInitialization(cancel, signalAt, initialized, func(v int) error { cleaned <- v; return nil }, time.Second)
		if result != 0 || !aborted || err != nil {
			t.Errorf("result=%d aborted=%v err=%v", result, aborted, err)
		}
	}()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("initialization context was not canceled")
	}
	initialized <- 7
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("initialization cleanup did not finish")
	}
	select {
	case v := <-cleaned:
		if v != 7 {
			t.Fatalf("cleaned resource = %d", v)
		}
	default:
		t.Fatal("late resource was not cleaned")
	}
}

func TestInitializationTimeoutStillOwnsLateResource(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	initialized := make(chan int, 1)
	signalAt := make(chan time.Time, 1)
	signalAt <- time.Now()
	cleaned := make(chan int, 1)
	_, aborted, err := awaitInitialization(cancel, signalAt, initialized, func(v int) error { cleaned <- v; return nil }, 20*time.Millisecond)
	if !aborted || err == nil {
		t.Fatalf("aborted=%v err=%v, want timeout", aborted, err)
	}
	if ctx.Err() == nil {
		t.Fatal("initialization context was not canceled")
	}
	initialized <- 9
	select {
	case v := <-cleaned:
		if v != 9 {
			t.Fatalf("cleaned resource = %d", v)
		}
	case <-time.After(time.Second):
		t.Fatal("late resource was not cleaned after timeout")
	}
}

type delayedListener struct {
	closed chan struct{}
	once   sync.Once
}

func (l *delayedListener) Accept() (net.Conn, error) { return nil, errors.New("closed") }
func (l *delayedListener) Close() error              { l.once.Do(func() { close(l.closed) }); return nil }
func (l *delayedListener) Addr() net.Addr            { return &net.TCPAddr{} }

func TestSignalDuringListenClosesLateListener(t *testing.T) {
	signalAt := make(chan time.Time, 1)
	at := time.Now()
	signalAt <- at
	release := make(chan struct{})
	listener := &delayedListener{closed: make(chan struct{})}
	result, gotAt, interrupted, err := listenWithSignal(signalAt, func() (net.Listener, error) { <-release; return listener, nil })
	if result != nil || !interrupted || err != nil || !gotAt.Equal(at) {
		t.Fatalf("result=%v interrupted=%v err=%v at=%v", result, interrupted, err, gotAt)
	}
	close(release)
	select {
	case <-listener.closed:
	case <-time.After(time.Second):
		t.Fatal("late listener was not closed")
	}
}
