# Terraform Provider Stile Manifest

This is an internal tool used by Stile Education for fetching "manifests" an internal concept of deployment artifacts from Buildkite. Feel free to modify it for your own purposes but it's currently configured for internal use and we don't have currently a need to generalise it.

Run the following command to build the provider

```shell
go build -o terraform-provider-stile-manifest
```

## Test sample configuration

First, build and install the provider.

```shell
make install
```

Then, run the following command to initialize the workspace and apply the sample configuration.

```shell
terraform init && terraform apply
```


## Publishing to the Terraform registry:

Follow instructions here for using "Using GoReleaser locally": https://www.terraform.io/docs/registry/providers/publishing.html

For the last step use `goreleaser release --config=config.yaml --rm-dist` to use the config.yaml config file. We need the config file because we renamed the project from `terraform-provider-stile-manifest` to `terraform-provider-stile`.