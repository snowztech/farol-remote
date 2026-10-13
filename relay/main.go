// The relay joins a Mac with the phones it paired, and passes bytes it cannot read.
// It keeps nothing: a room lives in memory for as long as someone is connected to it.
package main

import (
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	address := ":8080"
	if port := os.Getenv("PORT"); port != "" {
		address = ":" + port
	}
	push, err := newPusher()
	if err != nil {
		log.Fatal(err)
	}
	r := newRelay(push)
	go r.report(10 * time.Minute)
	log.Printf("farol relay on %s, notifications %s", address, map[bool]string{true: "on", false: "off"}[push != nil])
	// A client that opens a connection and never finishes asking is dropped, so it cannot hold a place for free.
	server := &http.Server{Addr: address, Handler: r.handler(), ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(server.ListenAndServe())
}
