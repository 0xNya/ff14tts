package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	log.SetFlags(log.Ltime | log.Lshortfile)

	config := NewConfigStore()
	config.Load()

	voices := NewVoiceStore()

	server := newServer(config, voices)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go startWebSocket(config, voices, ctx.Done())

	mux := server.Handler()

	httpServer := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	log.Println("Server starting at http://localhost:8080")
	go func() {
		<-sigCh
		log.Println("Shutting down...")
		cancel()
		httpServer.Shutdown(context.Background())
	}()

	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}
