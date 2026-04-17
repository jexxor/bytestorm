package transport

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"
)

const defaultLauncherShutdownTimeout = 10 * time.Second

type Launcher struct {
	servers         []Server
	shutdownTimeout time.Duration
}

func NewLauncher(servers ...Server) *Launcher {
	active := make([]Server, 0, len(servers))
	for _, server := range servers {
		if server != nil {
			active = append(active, server)
		}
	}

	return &Launcher{
		servers:         active,
		shutdownTimeout: defaultLauncherShutdownTimeout,
	}
}

func (l *Launcher) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	if len(l.servers) == 0 {
		return errors.New("launcher has no servers")
	}

	runCtx, stopSignals := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	errCh := make(chan error, len(l.servers))
	for _, server := range l.servers {
		srv := server
		go func() {
			errCh <- srv.Start(runCtx)
		}()
	}

	var runErr error
	select {
	case <-runCtx.Done():
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			runErr = err
		} else if runCtx.Err() == nil {
			runErr = errors.New("server stopped unexpectedly")
		}
		stopSignals()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), l.shutdownTimeout)
	defer cancel()

	for i := len(l.servers) - 1; i >= 0; i-- {
		if err := l.servers[i].Stop(shutdownCtx); err != nil &&
			!errors.Is(err, context.Canceled) &&
			!errors.Is(err, context.DeadlineExceeded) {
			zap.S().Warnf("Failed to stop server cleanly: %v", err)
			if runErr == nil {
				runErr = fmt.Errorf("failed to stop server: %w", err)
			}
		}
	}

	if runErr != nil {
		return runErr
	}

	if err := runCtx.Err(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}

	return nil
}
