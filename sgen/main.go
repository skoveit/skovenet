package main

import (
	"fmt"
	"os"
	"runtime"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "generate":
		cmdGenerate()
	case "keygen":
		cmdKeygen()
	case "list":
		cmdList()
	case "version":
		cmdVersion()
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func cmdGenerate() {
	var (
		targetOS   string
		targetArch string
		pubKey     string
		outPath    string
	)

	// Simple flag parsing for the generate subcommand.
	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--os":
			i++
			if i < len(args) {
				targetOS = args[i]
			}
		case "--arch":
			i++
			if i < len(args) {
				targetArch = args[i]
			}
		case "--key":
			i++
			if i < len(args) {
				pubKey = args[i]
			}
		case "--out":
			i++
			if i < len(args) {
				outPath = args[i]
			}
		case "--help", "-h":
			printGenerateUsage()
			return
		default:
			fmt.Fprintf(os.Stderr, "unknown flag: %s\n", args[i])
			printGenerateUsage()
			os.Exit(1)
		}
	}

	// Validate required flags.
	if targetOS == "" || targetArch == "" {
		fmt.Fprintln(os.Stderr, "error: --os and --arch are required")
		printGenerateUsage()
		os.Exit(1)
	}

	// If no key provided, auto-generate a keypair.
	if pubKey == "" {
		kp, err := GenerateKeyPair()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		pubKey = kp.PublicKey
		PrintKeyPair(kp)
		fmt.Println("[!] SAVE your private key — if you lose it, you lose control of every agent built with this key.")
		fmt.Println()
	}

	// Default output path.
	if outPath == "" {
		outPath = fmt.Sprintf("agent-%s-%s", targetOS, targetArch)
		if targetOS == "windows" {
			outPath += ".exe"
		}
	}

	printBanner()

	cfg := &BuildConfig{
		OS:         targetOS,
		Arch:       targetArch,
		PublicKey:  pubKey,
		OutputPath: outPath,
	}

	if err := Build(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "\n[✗] %v\n", err)
		os.Exit(1)
	}
}

func cmdKeygen() {
	kp, err := GenerateKeyPair()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	PrintKeyPair(kp)
}

func cmdList() {
	fmt.Println()
	fmt.Println("  Supported Platforms")
	fmt.Println("  ───────────────────────────────")
	fmt.Printf("  %-12s %-10s\n", "OS", "ARCH")
	fmt.Println("  ───────────────────────────────")
	for _, p := range supportedPlatforms {
		fmt.Printf("  %-12s %-10s\n", p.OS, p.Arch)
	}
	fmt.Println("  ───────────────────────────────")
	fmt.Printf("  Total: %d combinations\n\n", len(supportedPlatforms))
}

func cmdVersion() {
	fmt.Printf("sgen v%s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
}

func printBanner() {
	fmt.Println("  ┌──────────────────────────────────────┐")
	fmt.Println("  │         sgen · SkoveNet Agent        │")
	fmt.Println("  │              Generator               │")
	fmt.Printf("  │             v%-24s│\n", version)
	fmt.Println("  └──────────────────────────────────────┘")
	fmt.Println()
}

func printUsage() {
	fmt.Println("sgen — SkoveNet Agent Generator")
	fmt.Println()
	fmt.Println("Usage: sgen <command> [flags]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  generate    Generate an agent binary for a target platform")
	fmt.Println("  keygen      Generate a new Ed25519 keypair")
	fmt.Println("  list        List supported OS/architecture combinations")
	fmt.Println("  version     Show sgen version info")
	fmt.Println("  help        Show this help message")
	fmt.Println()
	fmt.Println("Run 'sgen <command> --help' for command-specific usage.")
}

func printGenerateUsage() {
	fmt.Println()
	fmt.Println("Usage: sgen generate [flags]")
	fmt.Println()
	fmt.Println("Flags:")
	fmt.Println("  --os <os>          Target OS: linux, windows, darwin          (required)")
	fmt.Println("  --arch <arch>      Target architecture: amd64, arm64, 386, arm (required)")
	fmt.Println("  --key <base64>     Ed25519 public key (auto-generated if omitted)")
	fmt.Println("  --out <path>       Output path (default: agent-<os>-<arch>[.exe])")
	fmt.Println()
	fmt.Println("If --key is omitted, a new Ed25519 keypair is generated automatically.")
}
