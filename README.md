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

- Import the GPG key that will be used to sign the release binaries: `gpg --import <key file>`
  Check it worked with: `gpg --list-keys`

- Get a GitHub token with `repo:public_repo` permission and set the
  env var `GITHUB_TOKEN` to that value.

- Sign a random file in order to cache the GPG key passphrase, eg:
  `gpg --armor --detach-sign config.yaml`
  (goreleaser doesn't support keys with passphrases, so we need to sign a
  random file so that we can input the passphrase and prevent it being
  interactively demanded.)

- Clean the repo: `git clean -fxd`

- Release! `goreleaser release --config config.yaml`



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
