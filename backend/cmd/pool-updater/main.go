package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/poolupdate"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", poolupdate.DefaultConfigPath, "Root-owned host updater JSON configuration")
	bootstrap := flag.Bool("bootstrap", false, "Install socket access on the existing verified image and exit")
	clearRecovery := flag.Bool("clear-recovery", false, "Acknowledge recovery after verifying the current healthy service")
	flag.Parse()
	if flag.NArg() != 0 || (*bootstrap && *clearRecovery) {
		return errors.New("invalid updater arguments")
	}
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		return errors.New("pool updater must run as root on a Linux host")
	}
	cfg, err := poolupdate.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(cfg.StateDir, 0700); err != nil {
		return err
	}
	if err = os.Chmod(cfg.StateDir, 0700); err != nil {
		return err
	}
	lock, err := poolupdate.AcquireLock(filepath.Join(cfg.StateDir, "updater.lock"))
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	worker, err := poolupdate.NewWorker(cfg, poolupdate.NewRegistry(), poolupdate.ExecRunner{})
	if err != nil {
		return err
	}
	defer worker.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *clearRecovery {
		return worker.ClearRecovery(ctx)
	}
	socketDir := filepath.Dir(cfg.SocketPath)
	if err = os.MkdirAll(socketDir, 0750); err != nil {
		return err
	}
	if err = os.Chmod(socketDir, 0750); err != nil {
		return err
	}
	if err = os.Chown(socketDir, 0, cfg.SocketGID); err != nil {
		return err
	}
	if *bootstrap {
		return worker.Bootstrap(ctx)
	}
	if info, statErr := os.Lstat(cfg.SocketPath); statErr == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return errors.New("refusing to replace a non-socket updater path")
		}
		if err = os.Remove(cfg.SocketPath); err != nil {
			return err
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	listener, err := net.Listen("unix", cfg.SocketPath)
	if err != nil {
		return err
	}
	defer func() { _ = listener.Close(); _ = os.Remove(cfg.SocketPath) }()
	if err = os.Chmod(cfg.SocketPath, 0660); err != nil {
		return err
	}
	if err = os.Chown(cfg.SocketPath, 0, cfg.SocketGID); err != nil {
		return err
	}
	server := &http.Server{Handler: poolupdate.Handler(worker), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 65 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
		worker.Close()
		return nil
	}
}
