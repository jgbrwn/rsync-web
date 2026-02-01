package main

import (
	"flag"
	"fmt"
	"os"

	"srv.exe.dev/srv"
)

var (
	flagListenAddr = flag.String("listen", "127.0.0.1:8000", "address to listen on")
	flagWorkDir    = flag.String("dir", "", "working directory (default: current directory)")
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	flag.Parse()
	
	workDir := *flagWorkDir
	if workDir == "" {
		var err error
		workDir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}
	}
	
	server, err := srv.New("rsync-web.db", workDir)
	if err != nil {
		return fmt.Errorf("create server: %w", err)
	}
	return server.Serve(*flagListenAddr)
}
