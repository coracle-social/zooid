package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	"zooid/zooid"
)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	port := zooid.Env("PORT")
	srv := &http.Server{
		Addr: fmt.Sprintf(":%s", port),
		Handler: http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				instance, exists := zooid.Dispatch(r.Host)
				if exists {
					instance.Relay.ServeHTTP(w, r)
				} else {
					http.Error(w, "Not Found", http.StatusNotFound)
				}
			},
		),
	}

	go func() {
		fmt.Printf("running on :%s\n", port)
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v\n", err)
		}
	}()

	go zooid.Start()

	<-shutdown

	fmt.Println("\nShutting down gracefully...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v\n", err)
	}
}
