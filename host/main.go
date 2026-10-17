// farol-host lets a paired phone see and type into the tmux sessions of a machine that stays on,
// the way Farol on a Mac does for its own sessions. It talks to the phone through the relay, which cannot read any of it.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/mdp/qrterminal/v3"
)

const usage = `usage: farol-host [-relay wss://your.relay] [command]

The Farol Remote CLI, for a machine that stays on. It lets a paired phone see and type into every tmux session here.
fh is a shorter name for it.

  setup           Make this machine ready: connect your agents, keep farol-host running, pair a phone.
  run             Let paired phones in, for as long as it runs. The service setup adds runs it for you.
  pair            Show a code to scan from Farol on a phone. farol-host has to be running.
  devices         List the phones this machine lets in.
  forget <name>   Stop letting a phone in.
  status <word>   Say what the agent in this session is doing: start, working, waiting, done or clear.
                  An agent's hooks call it, with what the agent gave the hook on its input.
                  Outside tmux it does nothing, so the hooks are safe anywhere.
  version         Say which release this is. So do --version and -v.
`

// Set to the release's tag when it is built for one.
var version = "dev"

func main() {
	relay := flag.String("relay", env("FAROL_RELAY", "wss://relay.farol.sh"), "the relay that joins this host with its phones")
	asked := flag.Bool("version", false, "say which release this is")
	flag.BoolVar(asked, "v", false, "say which release this is")
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	flag.Parse()
	if *asked {
		fmt.Println(version)
		return
	}
	if !strings.Contains(*relay, "://") {
		*relay = "wss://" + *relay
	}
	switch flag.Arg(0) {
	case "version":
		fmt.Println(version)
		return
	case "status":
		// A hook must never fail the agent, so only a word that is not a status is an error.
		if _, known := statuses[flag.Arg(1)]; !known && flag.Arg(1) != "subagent-stop" {
			fmt.Fprintf(os.Stderr, "farol-host: unknown status %s\n\n%s", flag.Arg(1), usage)
			os.Exit(64)
		}
		if pane := os.Getenv("TMUX_PANE"); pane != "" {
			setStatus(pane, flag.Arg(1), hookInput())
		}
		return
	}

	store, err := openStore()
	if err != nil {
		log.Fatal(err)
	}
	key, err := store.key()
	if err != nil {
		log.Fatalf("this host's key could not be read: %v", err)
	}

	switch flag.Arg(0) {
	case "":
		// ponytail: services set up before `run` existed start farol-host with no command and no terminal. Drop once they are all rewritten by setup.
		if info, err := os.Stdout.Stat(); err != nil || info.Mode()&os.ModeCharDevice == 0 {
			letPhonesIn(store, *relay, key)
			return
		}
		fmt.Print(usage, "\n")
		running := "farol-host is running."
		if store.lock() {
			running = "farol-host is not running. setup starts it, and keeps it running."
		}
		fmt.Println(running, paired(store.devices()))
	case "run":
		letPhonesIn(store, *relay, key)
	case "setup":
		if setup(*relay) {
			if len(store.devices()) == 0 {
				pair(store, *relay, key)
			} else {
				fmt.Println(paired(store.devices()))
			}
		}
	case "pair":
		pair(store, *relay, key)
	case "devices":
		for _, d := range store.devices() {
			fmt.Println(d.Name)
		}
	case "forget":
		kept := []device{}
		for _, d := range store.devices() {
			if d.Name != flag.Arg(1) {
				kept = append(kept, d)
			}
		}
		if err := store.saveDevices(kept); err != nil {
			log.Fatal(err)
		}
	default:
		flag.Usage()
		os.Exit(64)
	}
}

// hookInput is what an agent gave its hook on its input, which Claude Code and Codex write as JSON and close.
// A terminal or an input left open is never waited on for more than a moment, since a hook must not hold the agent up.
func hookInput() []byte {
	if info, err := os.Stdin.Stat(); err != nil || info.Mode()&os.ModeCharDevice != 0 {
		return nil
	}
	read := make(chan []byte, 1)
	go func() {
		// An approval for a large file carries the whole file.
		input, _ := io.ReadAll(io.LimitReader(os.Stdin, 16<<20))
		read <- input
	}()
	select {
	case input := <-read:
		return input
	case <-time.After(300 * time.Millisecond):
		return nil
	}
}

func env(name, otherwise string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return otherwise
}

func letPhonesIn(store store, relay string, key deviceKey) {
	if _, err := exec.LookPath("tmux"); err != nil {
		log.Fatal("tmux is not installed, and it is what runs the sessions")
	}
	// Two hosts with one key would keep taking the relay's connection from each other.
	if !store.lock() {
		log.Fatal("farol-host is already running on this machine")
	}
	if len(store.devices()) == 0 {
		showCode(store, relay, key)
	}
	// First in the log, so a report that quotes the log says which release it was.
	log.Printf("farol-host %s", version)
	serve(newHost(key, store, newTmux(key.public)), relay+"/v1/rooms/"+room(key.public)+"/mac")
}

func paired(devices []device) string {
	if len(devices) == 0 {
		return "No phone is paired yet: pair shows a code to scan."
	}
	names := []string{}
	for _, d := range devices {
		names = append(names, d.Name)
	}
	return "Paired with " + strings.Join(names, ", ") + ". pair adds another phone."
}

// pair shows a code and waits for the running host to say a phone scanned it.
func pair(store store, relay string, key deviceKey) {
	before := store.devices()
	shown := showCode(store, relay, key)
	for time.Now().Before(shown.expires()) {
		time.Sleep(time.Second)
		if now := store.devices(); len(now) > 0 && find(before, now[len(now)-1].Key) == nil {
			fmt.Printf("Paired with %s.\n", now[len(now)-1].Name)
			return
		}
	}
	fmt.Println("The code ran out. Is farol-host running?")
	os.Exit(1)
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

func connect(h *host, address string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dialing, done := context.WithTimeout(ctx, 15*time.Second)
	conn, _, err := websocket.Dial(dialing, address, nil)
	done()
	if err != nil {
		// The error names the address it called, which ends in this machine's room. A log gets pasted into bug reports, and the room is for this machine and its phones alone.
		var failed *url.Error
		if errors.As(err, &failed) {
			err = failed.Err
		}
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
		look, ping := time.NewTicker(40*time.Millisecond), time.NewTicker(20*time.Second)
		defer look.Stop()
		defer ping.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-look.C:
				if h.due(time.Now()) {
					h.tick()
				}
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
