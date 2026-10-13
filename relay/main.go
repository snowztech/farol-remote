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
	r := &relay{rooms: map[string]*room{}}
	log.Printf("farol relay on %s", address)
	log.Fatal(http.ListenAndServe(address, r.handler()))
}
