// The relay joins a Mac with the phones it paired, and passes bytes it cannot read.
// It keeps nothing: a room lives in memory for as long as someone is connected to it.
package main

import (
	"log"
	"net/http"
	"os"
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
	r := &relay{rooms: map[string]*room{}, push: push}
	log.Printf("farol relay on %s, notifications %s", address, map[bool]string{true: "on", false: "off"}[push != nil])
	log.Fatal(http.ListenAndServe(address, r.handler()))
}
