package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/acore2026/amf/internal/nagent"
)

func main() {
	address := flag.String("addr", ":8088", "HTTP listen address")
	delay := flag.Duration("delay", 0, "artificial response delay")
	status := flag.Int("status", http.StatusOK, "HTTP status returned after validation")
	flag.Parse()

	server := &http.Server{
		Addr:              *address,
		Handler:           nagent.NewMockHandler(nagent.MockConfig{Delay: *delay, Status: *status, Logf: log.Printf}),
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       3 * time.Second,
		WriteTimeout:      3 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	log.Printf("NAgent mock listening on %s", *address)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
