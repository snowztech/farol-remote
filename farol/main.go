// farol is the command for a machine that stays on. farol host lets a paired phone see and type into the tmux sessions of a machine that stays on,
// the way Farol on a Mac does for its own sessions. It talks to the phone through the relay, which cannot read any of it.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/mdp/qrterminal/v3"
)

const usage = `usage: farol host [-relay wss://your.relay] [pair | devices | forget <name>]
       farol status working|waiting|done|clear|subagent-start|subagent-stop

farol host lets a paired phone see and type into every tmux session on this machine.

  farol host                 Let paired phones in. Shows a code when no phone is paired yet.
  farol host pair            Show a code to scan from Farol on a phone. The host has to be running.
  farol host devices         List the phones this host lets in.
  farol host forget <name>   Stop letting a phone in.

farol status reports what the agent in this session is doing, so the phone can show it.
An agent's hooks call it. Outside tmux it does nothing, so hooks that call it are safe anywhere.
`

func main() {
	flags := flag.NewFlagSet("farol host", flag.ExitOnError)
	relay := flags.String("relay", env("FAROL_RELAY", "wss://relay.farol.sh"), "the relay that joins this host with its phones")
	flags.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	switch {
	case len(os.Args) == 3 && os.Args[1] == "status":
		// A hook must never fail the agent, so only a word that is not a status is an error.
		if _, known := statuses[os.Args[2]]; !known && os.Args[2] != "subagent-stop" {
			fmt.Fprintf(os.Stderr, "farol: unknown status %s\n\n%s", os.Args[2], usage)
			os.Exit(64)
		}
		if pane := os.Getenv("TMUX_PANE"); pane != "" {
			setStatus(pane, os.Args[2])
		}
		return
	case len(os.Args) < 2 || os.Args[1] != "host":
		fmt.Fprint(os.Stderr, usage)
		os.Exit(64)
	}
	flags.Parse(os.Args[2:])
	if !strings.Contains(*relay, "://") {
		*relay = "wss://" + *relay
	}

	store, err := openStore()
	if err != nil {
		log.Fatal(err)
	}
	key, err := store.key()
	if err != nil {
		log.Fatalf("this host's key could not be read: %v", err)
	}

	switch flags.Arg(0) {
	case "":
		if _, err := exec.LookPath("tmux"); err != nil {
			log.Fatal("tmux is not installed, and it is what runs the sessions")
		}
		if len(store.devices()) == 0 {
			showCode(store, *relay, key)
		}
		serve(newHost(key, store, newTmux()), *relay+"/v1/rooms/"+room(key.public)+"/mac")
	case "pair":
		before := store.devices()
		shown := showCode(store, *relay, key)
		for time.Now().Before(shown.expires()) {
			time.Sleep(time.Second)
			if now := store.devices(); len(now) > 0 && find(before, now[len(now)-1].Key) == nil {
				fmt.Printf("Paired with %s.\n", now[len(now)-1].Name)
				return
			}
		}
		fmt.Println("The code ran out. Is farol host running?")
		os.Exit(1)
	case "devices":
		for _, d := range store.devices() {
			fmt.Println(d.Name)
		}
	case "forget":
		kept := []device{}
		for _, d := range store.devices() {
			if d.Name != flags.Arg(1) {
				kept = append(kept, d)
			}
		}
		if err := store.saveDevices(kept); err != nil {
			log.Fatal(err)
		}
	default:
		flags.Usage()
		os.Exit(64)
	}
}

func env(name, otherwise string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return otherwise
}

func showCode(store store, relay string, key deviceKey) offer {
	shown, err := store.newOffer(relay, key, time.Now())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Scan this from Farol on your phone. It works once, for two minutes.")
	fmt.Println("Whoever scans it can type into your sessions, so show it to nobody else.")
	qrterminal.GenerateHalfBlock(shown.link(), qrterminal.L, os.Stdout)
	fmt.Println(shown.link())
	return shown
}

// serve keeps a connection to the relay, and makes a new one when it is lost.
func serve(h *host, url string) {
	wait := time.Second
	for {
		started := time.Now()
		err := connect(h, url)
		log.Printf("not connected to the relay: %v", err)
		// Waits longer after each failure, so a host without network does not spin.
		if time.Since(started) > time.Minute {
			wait = time.Second
		}
		time.Sleep(wait)
		wait = min(wait*2, 30*time.Second)
	}
}

func connect(h *host, url string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dialing, done := context.WithTimeout(ctx, 15*time.Second)
	conn, _, err := websocket.Dial(dialing, url, nil)
	done()
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	// The relay's own limit, so a message it would pass is never refused here.
	conn.SetReadLimit(256 << 10)
	h.connected(func(message []byte) {
		writing, done := context.WithTimeout(ctx, 10*time.Second)
		defer done()
		if conn.Write(writing, websocket.MessageBinary, message) != nil {
			cancel()
		}
	})
	log.Print("connected to the relay")

	go func() {
		look, ping := time.NewTicker(500*time.Millisecond), time.NewTicker(20*time.Second)
		defer look.Stop()
		defer ping.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-look.C:
				h.tick()
			case <-ping.C:
				// A connection that died without saying so reads nothing and fails nothing. A ping with no answer is how we learn.
				waiting, done := context.WithTimeout(ctx, 10*time.Second)
				err := conn.Ping(waiting)
				done()
				if err != nil {
					cancel()
				}
			}
		}
	}()

	for {
		_, message, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		h.receive(message)
	}
}
