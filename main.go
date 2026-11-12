package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/yetlabs/terraform-provider-yet/internal/provider"
)

// The docs under docs/ are made from the schemas and examples/: never by hand.
//go:generate go tool tfplugindocs generate --provider-name yet

// version is set by the release build (GoReleaser); "dev" otherwise.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run the provider with support for debuggers like delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/yetlabs/yet",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err.Error())
	}
}
