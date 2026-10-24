// Command voiceentry runs the on-site voice entry: it connects a virtual
// device to the Linx cloud and serves a local web console.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/open-anywhere/open-anywhere/internal/voiceentry"
	"github.com/open-anywhere/open-anywhere/internal/voiceentry/voice"
)

func main() { os.Exit(command(os.Args[1:], os.Stderr, run)) }

// command keeps flag parsing and exit-code decisions testable without
// exiting the process or touching the network.
func command(args []string, output io.Writer, start func(voiceentry.Config) error) int {
	flags := flag.NewFlagSet("voiceentry", flag.ContinueOnError)
	flags.SetOutput(output)
	var cfg voiceentry.Config
	var mode string
	flags.StringVar(&cfg.Text, "text", "", "send one text turn instead of speaking")
	flags.StringVar(&cfg.Announce, "announce", "", "have Linx announce this text")
	flags.StringVar(&cfg.DevicePath, "device", ".device.json", "device credential file")
	flags.StringVar(&mode, "mode", "realtime", "listening mode: realtime, auto or manual")
	flags.BoolVar(&cfg.Serve, "serve", false, "stay running and serve the web console")
	flags.StringVar(&cfg.Listen, "listen", "127.0.0.1:8788", "control API listen address")
	flags.StringVar(&cfg.File, "file", "", "audio file to send (needs -tags 'opus nolibopusfile')")
	flags.StringVar(&cfg.Mic, "mic", "", "system microphone device name (needs -tags 'opus nolibopusfile')")
	flags.BoolVar(&cfg.NoPlay, "no-play", false, "do not play downlink speech locally")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(output, "voiceentry does not accept positional arguments")
		return 2
	}
	if !cfg.Serve && cfg.Text == "" && cfg.Announce == "" && cfg.File == "" && cfg.Mic == "" {
		fmt.Fprintln(output, "nothing to do: use -serve, or one of -text, -announce, -file, -mic")
		return 2
	}
	switch cfg.Mode = voice.Mode(mode); cfg.Mode {
	case voice.Realtime, voice.Auto, voice.Manual:
	default:
		fmt.Fprintf(output, "invalid -mode %q: want realtime, auto or manual\n", mode)
		return 2
	}
	if err := start(cfg); err != nil {
		fmt.Fprintln(output, err)
		return 1
	}
	return 0
}

func run(cfg voiceentry.Config) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	a, err := voiceentry.New(cfg)
	if err != nil {
		return err
	}
	return a.Run(ctx)
}
