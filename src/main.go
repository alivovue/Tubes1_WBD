package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"tubes1_wbd/backend/access"
)

func main() {
	bootstrap := flag.Bool("bootstrap-admin", false, "Create the first admin in an empty platform using SCHAKKER_BOOTSTRAP_* environment variables")
	generate := flag.Bool("generate-key", false, "Print a new base64 master key, then exit")
	flag.Parse()
	if *generate {
		var key [32]byte
		if _, err := rand.Read(key[:]); err != nil {
			log.Fatal("key generation failed")
		}
		fmt.Println(base64.StdEncoding.EncodeToString(key[:]))
		return
	}
	key, err := base64.StdEncoding.DecodeString(os.Getenv("SCHAKKER_MASTER_KEY"))
	if err != nil || len(key) != 32 {
		log.Fatal("SCHAKKER_MASTER_KEY must be a base64-encoded 32-byte secret; generate with go run . -generate-key")
	}
	mode := os.Getenv("APP_ENV")
	if mode == "" {
		mode = "development"
	}
	if mode != "development" && mode != "production" {
		log.Fatal("APP_ENV must be development or production")
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is required for the platform metadata database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	db, err := access.OpenDatabase(ctx, dsn)
	cancel()
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	service, err := access.NewService(db, access.DefaultConfig(key, mode == "production"))
	if err != nil {
		log.Fatal("invalid access configuration")
	}
	if *bootstrap {
		username := os.Getenv("SCHAKKER_BOOTSTRAP_USERNAME")
		name := os.Getenv("SCHAKKER_BOOTSTRAP_NAME")
		if name == "" {
			name = username
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := service.BootstrapAdmin(ctx, username, name, os.Getenv("SCHAKKER_BOOTSTRAP_PASSWORD")); err != nil {
			log.Fatal("bootstrap gagal: periksa schema, input (password 12–72 byte), dan pastikan platform belum memiliki akun")
		}
		log.Println("Platform Admin pertama berhasil dibuat.")
		return
	}
	handler, err := service.Handler()
	if err != nil {
		log.Fatal("cannot load portal templates")
	}
	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = "127.0.0.1:3000"
	}
	server := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	stop, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-stop.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}()
	log.Printf("Schakker listening on %s (%s)", addr, mode)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal("HTTP server stopped unexpectedly")
	}
	<-shutdownDone
}
