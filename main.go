package main

import (
	"os"

	"github.com/veil-net/veilnet"
	"github.com/veil-net/conflux/conflux"
	"github.com/alecthomas/kong"
)

var version = "1.0.5"

func main() {
	// Parse the CLI arguments
	var cli conflux.CLI
	ctx := kong.Parse(&cli, kong.Vars{"version": version})
	err := ctx.Run()
	if err != nil {
		veilnet.Logger.Sugar().Errorf("%v", err)
		os.Exit(1)
	}
}
