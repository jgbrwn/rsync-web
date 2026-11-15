package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"srv.exe.dev/srv"
)

type presetFlag []srv.Preset

func (p *presetFlag) String() string { return fmt.Sprint(*p) }
func (p *presetFlag) Set(v string) error {
	name, dest, ok := strings.Cut(v, "=")
	if !ok || strings.TrimSpace(name) == "" || strings.TrimSpace(dest) == "" {
		return fmt.Errorf("want NAME=DESTINATION, got %q", v)
	}
	*p = append(*p, srv.Preset{Name: strings.TrimSpace(name), Destination: strings.TrimSpace(dest)})
	return nil
}

var (
	flagListenAddr = flag.String("listen", "127.0.0.1:8000", "address to listen on")
	flagWorkDir    = flag.String("dir", "", "working directory (default: current directory)")
	flagPresetOpts = flag.String("preset-opts", "", "comma- or space-separated rsync options for presets and /api/push (default: safe copy flags, never --delete)")
	flagPushRoot   = flag.String("push-root", "", "directory under -dir whose direct children /api/push may send (e.g. media)")
	flagWebhook    = flag.String("webhook", "", "URL to POST a JSON summary to when a job finishes (secret: RSYNC_WEB_WEBHOOK_SECRET)")
	flagPresets    presetFlag
)

func init() {
	flag.Var(&flagPresets, "preset", "one-click destination NAME=DEST, e.g. Movies=host:media/movies/ (repeatable)")
}

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
	
	opts := srv.Options{
		Presets:       flagPresets,
		PushRoot:      *flagPushRoot,
		WebhookURL:    *flagWebhook,
		WebhookSecret: os.Getenv("RSYNC_WEB_WEBHOOK_SECRET"),
	}
	if *flagPresetOpts != "" {
		opts.PresetOptions = strings.FieldsFunc(*flagPresetOpts, func(r rune) bool { return r == ',' || r == ' ' })
	}
	server, err := srv.New("rsync-web.db", workDir, opts)
	if err != nil {
		return fmt.Errorf("create server: %w", err)
	}
	return server.Serve(*flagListenAddr)
}
