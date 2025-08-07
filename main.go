package main

import (
	"os"

	veilnet "github.com/VeilNet-PTY-LTD/veilnet"
	"github.com/VeilNet-PTY-LTD/veilnet-conflux/conflux"
	"github.com/alecthomas/kong"
)

var version = "1.0.3"

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
