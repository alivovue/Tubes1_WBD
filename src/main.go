package main

import (
	"log"
	"net/http"

	"tubes1_wbd/backend"
)

func main() {
	handler, err := backend.NewHandler(".")
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Server running on http://localhost:3000")
	log.Fatal(http.ListenAndServe(":3000", handler))
}
