# Terraform Provider Stile Manifest

This is an internal tool used by Stile Education for fetching "manifests" an internal concept of deployment artifacts from Buildkite. Feel free to modify it for your own purposes but it's currently configured for internal use and we don't have currently a need to generalise it.

Run the following command to build the provider

```shell
make build
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

- Tag the release (e.g. `git tag v0.0.11`)
- Push the tags to GitHub (`git push --tags`)
- Make sure you have the GPG key imported using `gpg --import <key file>`. To check this worked properly you should see the key in:
  - `gpg --list-keys`
  - `gpg --list-secret-keys`
- Get a GitHub token with `repo:public_repo` permission and set the
  env var `GITHUB_TOKEN` to that value
- Get the GPG key fingerprint:
  - `gpg --list-secret-keys` and copy it from the output
  - Set to the `GPG_FINGERPRINT` env var
- Follow instructions here for using "Using GoReleaser locally":
https://www.terraform.io/docs/registry/providers/publishing.html For
the last step use `goreleaser release --config=config.yaml --rm-dist`
to use the config.yaml config file. We need the config file because we
renamed the project from `terraform-provider-stile-manifest` to
`terraform-provider-stile`.
	- Running `goreleaser` the first time will fail, rerun that
      command that it fails on which will be something like: `gpg --local-user <fingerprint> --output dist/terraform-provider-stile_<version>_SHA256SUMS.sig --detach-sign dist/terraform-provider-stile_<version>_SHA256SUMS`.
	  You'll be prompted to enter the key's password.
	- Rerun `goreleaser` and the new version will be deployed!


## Local dev

You can build and test your changes to the provider like so:

* build the provider binary with `go build`

* write a Terraform conf file, eg: `~/.terraform.rc`, like so
  ```
  provider_installation {
    dev_overrides {
      "registry.terraform.io/StileEducation/stile" = "/path/to/dir/that/contains/provider/project"
    }
    direct {}
  }
  ```

* export an env var to use the conf file:
  ```
  export TF_CLI_CONFIG_FILE=~/.terraform.rc
  ```

* run Terraform commands like normal
