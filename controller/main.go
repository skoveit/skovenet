package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/skoveit/skovenet/pkg/ipc"
	"github.com/skoveit/skovenet/static"

	"github.com/peterh/liner"
)

var (
	client       *ipc.ControllerClient
	selectedPeer string
	peerList     []string
	peerCount    int
	mu           sync.RWMutex
	graphServer  net.Listener // graph web server
)

var commands = []string{"sign", "use", "run", "back", "send", "peers", "radar", "graph", "clear", "cls", "id", "help", "quit", "exit"}

func main() {
	var err error
	client, err = ipc.NewControllerClient()
	if err != nil {
		fmt.Println("No running agent found 🙁")
		os.Exit(1)
	}
	defer client.Close()

	// Get initial peer list
	refreshPeers()

	// Listen for async messages and events
	go handleAsyncMessages()
	go handleEvents()

	fmt.Println("Connected to agent")
	fmt.Println("Type 'help' for commands, TAB for completion")
	fmt.Println()

	// Setup liner
	line := liner.NewLiner()
	defer line.Close()

	line.SetCtrlCAborts(true)

	// Bash-style tab completion
	line.SetCompleter(func(input string) []string {
		return complete(input)
	})

	// Main loop
	for {
		prompt := getPrompt()
		input, err := line.Prompt(prompt)
		if err != nil {
			if err == liner.ErrPromptAborted {
				continue
			}
			break
		}

		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		line.AppendHistory(input)
		execute(input)
	}
}

func getPrompt() string {
	mu.RLock()
	defer mu.RUnlock()

	if selectedPeer != "" {
		short := selectedPeer
		if len(short) > 16 {
			short = short[:16]
		}
		return fmt.Sprintf("[%s]> ", short)
	}
	return fmt.Sprintf("[%d peers]> ", peerCount)
}

func complete(input string) []string {
	input = strings.TrimSpace(input)
	words := strings.Fields(input)

	// Complete commands
	if len(words) == 0 || (len(words) == 1 && !strings.HasSuffix(input, " ")) {
		prefix := ""
		if len(words) == 1 {
			prefix = words[0]
		}
		var matches []string
		for _, cmd := range commands {
			if strings.HasPrefix(cmd, prefix) {
				matches = append(matches, cmd)
			}
		}
		return matches
	}

	// Complete peer IDs for 'use' and 'send'
	cmd := words[0]
	if cmd == "use" || cmd == "send" {
		mu.RLock()
		peers := peerList
		mu.RUnlock()

		prefix := ""
		if len(words) >= 2 && !strings.HasSuffix(input, " ") {
			prefix = words[len(words)-1]
		}

		var matches []string
		for _, p := range peers {
			if strings.HasPrefix(p, prefix) {
				// Return full command with peer
				if cmd == "use" {
					matches = append(matches, "use "+p)
				} else {
					matches = append(matches, strings.Join(words[:len(words)-1], " ")+" "+p)
				}
			}
		}
		return matches
	}

	return nil
}

func execute(input string) {
	cmd, args := ipc.ParseInput(input)

	switch cmd {
	case "help":
		printHelp()

	case "use":
		if len(args) == 0 {
			selectPeer()
		} else {
			mu.Lock()
			selectedPeer = args[0]
			mu.Unlock()
			fmt.Printf("Selected peer: %s\n", selectedPeer)
		}

	case "back":
		mu.Lock()
		selectedPeer = ""
		mu.Unlock()
		fmt.Println("Deselected peer")

	case "run":
		mu.RLock()
		peer := selectedPeer
		mu.RUnlock()

		if peer == "" {
			fmt.Println("No peer selected. Use 'use <peerID>' first")
			return
		}
		if len(args) == 0 {
			fmt.Println("Usage: run <command>")
			return
		}
		sendArgs := append([]string{peer}, args...)
		resp, _ := client.Send("send", sendArgs...)
		fmt.Println(resp)

	case "send":
		if len(args) < 2 {
			fmt.Println("Usage: send <peerID> <command>")
			return
		}
		resp, _ := client.Send("send", args...)
		fmt.Println(resp)

	case "peers":
		refreshPeers()
		resp, _ := client.Send("peers")
		fmt.Println(resp)

	case "radar":
		runRadar()

	case "clear", "cls":
		fmt.Print("\033[H\033[2J")

	case "id":
		resp, _ := client.Send("id")
		fmt.Println(resp)

	case "graph":
		if len(args) == 0 {
			fmt.Println("Usage: graph on | graph off")
			return
		}
		switch args[0] {
		case "on":
			graphOn()
		case "off":
			graphOff()
		default:
			fmt.Println("Usage: graph on | graph off")
		}

	case "sign":
		handleSign(args)

	case "quit", "exit":
		client.Send("quit")
		fmt.Println("Goodbye!")
		os.Exit(0)

	default:
		fmt.Printf("Unknown command: %s (type 'help' for commands)\n", cmd)
	}
}

// RadarResult matches agent's struct
type RadarResult struct {
	PeerID    string `json:"peer_id"`
	Latency   int64  `json:"latency_ms"`
	Timestamp int64  `json:"timestamp"`
}

func runRadar() {
	fmt.Println()
	printRadarAnimation()

	// Request radar scan from agent
	resp, err := client.Send("radar", "3s")
	if err != nil {
		fmt.Println("Radar failed:", err)
		return
	}

	var results []RadarResult
	if err := json.Unmarshal([]byte(resp), &results); err != nil {
		fmt.Println("Failed to parse radar results")
		return
	}

	// Sort by latency
	sort.Slice(results, func(i, j int) bool {
		return results[i].Latency < results[j].Latency
	})

	printRadarResults(results)
}

func printRadarAnimation() {
	frames := []string{
		"    ◜    ",
		"     ◝   ",
		"      ◞  ",
		"       ◟ ",
		"      ◞  ",
		"     ◝   ",
		"    ◜    ",
		"   ◟     ",
		"  ◞      ",
		" ◝       ",
		"  ◞      ",
		"   ◟     ",
	}

	fmt.Print("  Scanning network ")
	for i := 0; i < 12; i++ {
		fmt.Printf("\r  Scanning network %s", frames[i%len(frames)])
		time.Sleep(250 * time.Millisecond)
	}
	fmt.Print("\r                              \r")
}

func printRadarResults(results []RadarResult) {
	if len(results) == 0 {
		fmt.Println("  ╭─────────────────────────────────╮")
		fmt.Println("  │  📡 RADAR - No nodes detected   │")
		fmt.Println("  ╰─────────────────────────────────╯")
		return
	}

	// Header
	fmt.Println("  ╭───────────────────────────────────────────────────────────────────────────────────────╮")
	fmt.Printf("  │                           📡 RADAR SCAN - %d node(s) detected                         │\n", len(results))
	fmt.Println("  ├───────────────────────────────────────────────────────────────────────────────────────┤")

	// Results
	for i, r := range results {
		// Signal strength based on latency
		var signal string
		switch {
		case r.Latency < 50:
			signal = "████▓░░ EXCELLENT"
		case r.Latency < 100:
			signal = "███▓░░░ GOOD"
		case r.Latency < 200:
			signal = "██▓░░░░ FAIR"
		case r.Latency < 500:
			signal = "█▓░░░░░ WEAK"
		default:
			signal = "▓░░░░░░ POOR"
		}

		fmt.Printf("  │  %2d. %-20s  %4dms  %s  │\n", i+1, r.PeerID, r.Latency, signal)
	}

	// Footer
	fmt.Println("  ╰───────────────────────────────────────────────────────────────────────────────────────╯")
	fmt.Println()
}

func graphOn() {
	if graphServer != nil {
		fmt.Println("Graph server already running")
		return
	}

	// Find available port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Println("Failed to start server:", err)
		return
	}
	graphServer = listener
	port := listener.Addr().(*net.TCPAddr).Port
	url := fmt.Sprintf("http://127.0.0.1:%d", port)

	// Setup HTTP handlers
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write(static.GraphHTML)
	})
	mux.HandleFunc("/api/topology", func(w http.ResponseWriter, r *http.Request) {
		data, err := client.Send("topology")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(data))
	})

	// Start server in background
	go http.Serve(listener, mux)

	fmt.Printf("📡 Graph server: %s\n", url)
	fmt.Println("Use /?interval=5 for auto-refresh every 5s")
	openBrowser(url)
}

func graphOff() {
	if graphServer == nil {
		fmt.Println("Graph server not running")
		return
	}
	graphServer.Close()
	graphServer = nil
	fmt.Println("Graph server stopped")
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Start()
}

func selectPeer() {
	mu.RLock()
	peers := peerList
	mu.RUnlock()

	if len(peers) == 0 {
		fmt.Println("No peers connected")
		return
	}

	fmt.Println("Connected peers:")
	for i, p := range peers {
		fmt.Printf("  %d. %s\n", i+1, p)
	}
	fmt.Println("\nUse 'use <peerID>' or TAB to complete")
}

func refreshPeers() {
	resp, err := client.Send("peerlist")
	if err != nil {
		return
	}

	var peers []string
	if json.Unmarshal([]byte(resp), &peers) == nil {
		mu.Lock()
		peerList = peers
		peerCount = len(peers)
		mu.Unlock()
	}
}

func handleAsyncMessages() {
	for msg := range client.AsyncMessages() {
		fmt.Printf("\n%s\n", msg)
	}
}

func handleEvents() {
	for event := range client.Events() {
		switch event.Type {
		case "peer_connected":
			refreshPeers()
			short := event.Data
			if len(short) > 16 {
				short = short[:16]
			}
			fmt.Printf("\n[+] Peer connected: %s\n", short)
		case "peer_disconnected":
			refreshPeers()
			short := event.Data
			if len(short) > 16 {
				short = short[:16]
			}
			fmt.Printf("\n[-] Peer disconnected: %s\n", short)
		}
	}
}

func handleSign(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: sign <private_key> OR sign -path <file_path>")
		return
	}

	var privateKey string

	if args[0] == "-path" {
		if len(args) < 2 {
			fmt.Println("Usage: sign -path <file_path>")
			return
		}
		data, err := os.ReadFile(args[1])
		if err != nil {
			fmt.Printf("Failed to read key file: %v\n", err)
			return
		}
		privateKey = strings.TrimSpace(string(data))
	} else {
		privateKey = args[0]
	}

	resp, err := client.Send("sign", privateKey)
	if err != nil {
		fmt.Printf("Failed to sign: %v\n", err)
		return
	}

	if resp == "signed" {
		fmt.Println("✅ Signed in as operator")
	} else {
		fmt.Println(resp)
	}
}

func printHelp() {
	fmt.Println(`
Commands:
  sign <key>         Sign in with operator private key
  sign -path <file>  Sign in with key from file
  use [peerID]       Select target peer (TAB completes peer ID)
  run <command>      Execute command on selected peer
  back               Deselect peer
  send <id> <cmd>    Send command to specific peer
  peers              List connected peers
  radar              Scan entire network for all nodes
  graph on           Start topology web viewer
  graph off          Stop topology web viewer
  clear, cls         Clear terminal screen
  id                 Show node ID
  help               Show this help
  quit               Exit`)
}
