package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"tubes1_wbd/backend"
)

func main() {
	handler, err := backend.NewHandler(".")
	if err != nil {
		log.Fatal(err)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	server := &http.Server{Addr: ":" + port, Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 3 * time.Minute, IdleTimeout: 90 * time.Second}
	log.Printf("Server running on http://localhost:%s", port)
	log.Fatal(server.ListenAndServe())
}
