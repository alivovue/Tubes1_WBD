package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"tubes1_wbd_admin/backend"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	handler, err := backend.NewHandler(".")
	if err != nil {
		return err
	}
	if closer, ok := handler.(io.Closer); ok {
		defer closer.Close()
	}
	port := os.Getenv("SCHAKKER_ADMIN_PORT")
	if port == "" {
		port = "3001"
	}
	server := &http.Server{Addr: ":" + port, Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 3 * time.Minute, IdleTimeout: 90 * time.Second}
	internalAddr := os.Getenv("SCHAKKER_ADMIN_INTERNAL_ADDR")
	if internalAddr == "" {
		internalAddr = "127.0.0.1:3002"
	}
	host, _, err := net.SplitHostPort(internalAddr)
	if err != nil {
		return fmt.Errorf("invalid internal listen address")
	}
	cert, key := os.Getenv("SCHAKKER_ADMIN_INTERNAL_TLS_CERT"), os.Getenv("SCHAKKER_ADMIN_INTERNAL_TLS_KEY")
	ip := net.ParseIP(host)
	if (cert == "") != (key == "") {
		return errors.New("supply both internal TLS certificate and key")
	}
	if cert == "" && (os.Getenv("SCHAKKER_ADMIN_ENV") == "production" || ip == nil || !ip.IsLoopback()) {
		return errors.New("internal API requires TLS in production and on non-loopback addresses")
	}
	private := &http.Server{Addr: internalAddr, Handler: handler.(interface{ InternalHandler() http.Handler }).InternalHandler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second}
	stopped := make(chan error, 2)
	go func() { stopped <- server.ListenAndServe() }()
	go func() {
		if cert != "" {
			stopped <- private.ListenAndServeTLS(cert, key)
		} else {
			stopped <- private.ListenAndServe()
		}
	}()
	defer server.Close()
	defer private.Close()
	log.Printf("Admin server running on http://localhost:%s", port)

	select {
	case err := <-stopped:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := private.Shutdown(shutdown); err != nil {
			return err
		}
		if err := server.Shutdown(shutdown); err != nil {
			server.Close()
			return err
		}
		return nil
	}
}
