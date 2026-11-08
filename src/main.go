// Command scripts-mcp is an MCP server that serves shell scripts as tools.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var version = "dev" // set by build.sh

func main() {
	log.SetFlags(0)
	log.SetPrefix("scripts-mcp: ")
	fs := flag.NewFlagSet("scripts-mcp", flag.ExitOnError)
	setup := fs.Bool("setup", false, "clone missing git sources, fetch and check out the configured ref of existing ones, and exit")
	showVersion := fs.Bool("version", false, "print the version and exit")
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), "Usage:\n"+
			"  scripts-mcp <config_file>          run the MCP server (stdio)\n"+
			"  scripts-mcp --setup <config_file>  set up git sources and exit\n\n"+
			"Options:\n")
		fs.VisitAll(func(f *flag.Flag) {
			fmt.Fprintf(fs.Output(), "  --%s\n    \t%s\n", f.Name, f.Usage)
		})
	}
	fs.Parse(os.Args[1:])
	if *showVersion {
		fmt.Println("scripts-mcp", version)
		return
	}
	if fs.NArg() != 1 {
		fmt.Fprint(fs.Output(), "error: expected one config file; put options before it\n\n")
		fs.Usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, fs.Arg(0), *setup); err != nil {
		stop()
		log.Fatal(err)
	}
}

func run(ctx context.Context, configFile string, setup bool) error {
	cfg, err := LoadConfig(configFile)
	if err != nil {
		return err
	}
	if setup {
		return cfg.UpdateRepos(ctx)
	}
	if err := cfg.RequireRepos(configFile); err != nil {
		return err
	}
	scripts, err := LoadScripts(cfg)
	if err != nil {
		return err
	}

	log.Printf("serving %d scripts", len(scripts))
	err = NewServer(ctx, cfg, scripts).Run(ctx, &mcp.StdioTransport{})
	if ctx.Err() != nil {
		return nil // stopped by signal
	}
	return err
}
