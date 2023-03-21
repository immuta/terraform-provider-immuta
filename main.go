package terraform_provider_immuta

import (
	"context"
	"flag"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/instacart/terraform-provider-immuta/immuta"
	"log"
)

var (
	// these will be set by the goreleaser configuration
	// to appropriate values for the compiled binary.
	version string = "dev"

	// goreleaser can pass other information to the main package, such as the specific commit
	// https://goreleaser.com/cookbooks/using-main.version/
)

func main() {
	var debug bool

	flag.BoolVar(&debug, "debug", false, "set to true to run the provider with support for debuggers like delve")
	flag.Parse()

	opts := providerserver.ServeOpts{
		Debug: debug,
	}

	err := providerserver.Serve(
		context.Background(),
		immuta.NewProvider(version),
		opts,
	)

	if err != nil {
		log.Fatal(err.Error())
	}
}
