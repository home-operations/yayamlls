package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/home-operations/yayamlls/internal/lint"
	"github.com/home-operations/yayamlls/internal/lsp"
	"github.com/home-operations/yayamlls/internal/render"
	"github.com/home-operations/yayamlls/internal/render/flate"
	"github.com/home-operations/yayamlls/internal/render/subprocess"
	"github.com/tliron/commonlog"
	_ "github.com/tliron/commonlog/simple"
	"github.com/tliron/glsp/server"
)

var (
	version = "0.0.0-dev"
	commit  = ""
)

func main() {
	// Subcommands run one-shot and exit; absent one, the binary is the
	// stdio language server.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "validate", "lint":
			os.Exit(lint.Run(os.Args[2:], os.Stdout, os.Stderr))
		}
	}

	var (
		showVersion bool
		logFile     string
		verbosity   int
	)
	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	flag.StringVar(&logFile, "log-file", "", "write log output to this file instead of stderr")
	flag.IntVar(&verbosity, "v", 0, "log verbosity (0=silent, 1=info, 2+=debug)")
	flag.Parse()

	if showVersion {
		if commit != "" {
			fmt.Printf("%s (%s)\n", version, commit)
		} else {
			fmt.Println(version)
		}
		return
	}

	var logPath *string
	if logFile != "" {
		logPath = &logFile
	}
	commonlog.Configure(verbosity, logPath)

	registry := render.NewRegistry()
	registry.Register(flate.New())
	registry.SetFactory(subprocess.FromConfig)

	s := lsp.New(version, registry)
	srv := server.NewServer(s.Handler(), "yayamlls", false)
	if err := srv.RunStdio(); err != nil {
		fmt.Fprintf(os.Stderr, "server stopped with error: %v\n", err)
		os.Exit(1)
	}
}
