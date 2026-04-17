package transport

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestLauncherRunStopsServersOnContextCancel(t *testing.T) {
	t.Parallel()

	s1 := newLauncherTestServer(nil)
	s2 := newLauncherTestServer(nil)

	launcher := NewLauncher(s1, s2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- launcher.Run(ctx)
	}()

	waitForSignal(t, s1.startCalled, "server 1 start")
	waitForSignal(t, s2.startCalled, "server 2 start")

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Launcher.Run returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting Launcher.Run to return")
	}

	waitForSignal(t, s1.stopCalled, "server 1 stop")
	waitForSignal(t, s2.stopCalled, "server 2 stop")
}

func TestLauncherRunReturnsStartError(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	failing := newLauncherTestServer(boom)
	other := newLauncherTestServer(nil)

	launcher := NewLauncher(failing, other)

	err := launcher.Run(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("Launcher.Run() error = %v, want %v", err, boom)
	}

	waitForSignal(t, other.stopCalled, "other server stop")
}

type launcherTestServer struct {
	startErr    error
	startCalled chan struct{}
	stopCalled  chan struct{}
	stopOnce    sync.Once
}

func newLauncherTestServer(startErr error) *launcherTestServer {
	return &launcherTestServer{
		startErr:    startErr,
		startCalled: make(chan struct{}),
		stopCalled:  make(chan struct{}),
	}
}

func (s *launcherTestServer) Start(ctx context.Context) error {
	close(s.startCalled)
	if s.startErr != nil {
		return s.startErr
	}

	select {
	case <-ctx.Done():
		return nil
	case <-s.stopCalled:
		return nil
	}
}

func (s *launcherTestServer) Stop(ctx context.Context) error {
	s.stopOnce.Do(func() {
		close(s.stopCalled)
	})
	return nil
}

func waitForSignal(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for %s", label)
	}
}
