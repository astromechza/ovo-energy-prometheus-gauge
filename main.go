package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"go.uber.org/zap"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// These are "build-time" vars that will be filled in during the final build
var (
	version   = "unknown"
	buildDate = "unknown"
)

const mainUsage = `ovo-energy-prometheus-gauge is a small web server that continuously connects to OVO energy
to fetch the latest gas and energy readings. These metrics are then presented over a port for prometheus to scan as gauges.'

Options:
`

type AccountInfo struct {
	AccountNumber string `json:"accountNumber"`
	Username      string `json:"username"`
	Password      string `json:"password"`
}

func mainInner() error {
	// Define and parse the top level cli flags - each subcommand has their own flag set too!
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	versionFlag := fs.Bool("version", false, "Show version information")
	debugFlag := fs.Bool("debug", false, "Show debug logs")
	configFlag := fs.String("config", "/config.json", "Json account config file (default: /config.json)")
	updateInterval := fs.Duration("interval", time.Minute*30, "Interval to scan ovo (default: 30m)")

	fs.Usage = func() {
		_, _ = fmt.Fprint(os.Stderr, mainUsage)
		fs.PrintDefaults()
	}
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}
	if *versionFlag {
		fmt.Printf("Version:     %s\n", version)
		fmt.Printf("Build Date:  %s\n", buildDate)
		fmt.Printf("URL:         https://github.com/astromechza/ovo-energy-prometheus-gauge\n")
		return nil
	}
	if fs.NArg() != 0 {
		fs.Usage()
		_, _ = fmt.Fprintf(os.Stderr, "\n")
		return fmt.Errorf("no positional arguments expected")
	}
	if *updateInterval < time.Second*10 {
		return fmt.Errorf("update interval must be at least 10 seconds")
	}

	logger, _ := zap.NewProduction()
	if *debugFlag {
		logger, _ = zap.NewDevelopment()
	}
	zap.ReplaceGlobals(logger)

	conf := &AccountInfo{}
	zap.S().Infow("loading config", "config", *configFlag)
	confFile, err := os.Open(*configFlag)
	if err != nil {
		return err
	}
	defer confFile.Close()

	zap.S().Infow("decoding config", "config", *configFlag)
	if err = json.NewDecoder(confFile).Decode(conf); err != nil {
		return err
	}
	if conf.AccountNumber == "" {
		return fmt.Errorf("config accountNumber is missing")
	}
	if conf.Username == "" {
		return fmt.Errorf("config username is missing")
	}
	if conf.Password == "" {
		return fmt.Errorf("config password is missing")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ovo := Ovo{AccountInfo: conf}

	go func() {
		ticker := time.NewTicker(*updateInterval)
		for {
			if err := ovo.Scan(); err != nil {
				zap.S().Errorw("failed to scan ovo", "err", err)
			}

			select {
			case <-ticker.C:
				zap.L().Info("update tick")
			case <-ctx.Done():
				zap.L().Info("closing background routine")
				return
			}
		}
	}()

	http.Handle("/metrics", promhttp.Handler())
	addr := ":8080"
	zap.S().Infow("starting server", "address", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		return err
	}
	return nil
}

func main() {
	if err := mainInner(); err != nil {
		zap.S().Errorw("failed", "err", err)
		os.Exit(1)
	}
}
