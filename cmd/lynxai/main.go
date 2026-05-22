// Command lynxai is the self-hostable AI-native headless browser server.
//
// v1 exposes:
//   lynxai serve [--addr :7878] [--data-dir ~/.lynxai] [--bridle-config <path>]
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		fmt.Println("serve: not yet implemented")
		os.Exit(1)
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `Usage: lynxai <subcommand> [flags]

Subcommands:
  serve   Run the lynxai HTTP server
  help    Show this message`)
}
