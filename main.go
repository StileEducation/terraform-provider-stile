package main

import (
	"os"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/plugin"

	"terraform-provider-stile/internal/listmanifests"
	"terraform-provider-stile/stile"
)

func main() {
	// CLI subcommand mode. When invoked as
	// `terraform-provider-stile list-manifests <tf-dir>`, bypass the
	// standard plugin server and run the subcommand instead. Used by
	// stops (and potentially other tooling) to discover which BFP
	// manifests a given Terraform configuration consumes, without
	// needing to run `terraform plan` (which would fail when the
	// manifest hasn't yet been uploaded).
	if len(os.Args) >= 2 && os.Args[1] == "list-manifests" {
		os.Exit(listmanifests.Run(os.Args[2:]))
	}

	plugin.Serve(&plugin.ServeOpts{
		ProviderFunc: func() *schema.Provider {
			return stile.Provider()
		},
	})
}
