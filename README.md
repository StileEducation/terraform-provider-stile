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

- Find the key's fingerprint using `gpg --list-secret-keys`

- Export an env var using the fingerprint: `export GPG_FINGERPRINT=<fingerprint>`

- Get a GitHub token with `repo:public_repo` permission and set the
  env var `GITHUB_TOKEN` to that value.

- Sign a random file in order to cache the GPG key passphrase, eg:
  `gpg --armor --detach-sign config.yaml`
  (goreleaser doesn't support keys with passphrases, so we need to sign a
  random file so that we can input the passphrase and prevent it being
  interactively demanded. This failure mode looks like: `signing failed: Inappropriate ioctl for device`)

- Clean the repo: `git clean -fxd`

- Release! `goreleaser release --config config.yaml`

- Wait a minute or so for the Terraform registry to pick up the change and
  publish the new version.


### Debugging releases

The most opaque part of this process is the last step: waiting for the
Terraform registry. If you get anything wrong, it will silently just do
nothing. Grab bag of things to check: signing worked correctly, the
Terraform registry manifest file is present, the tag name is not also a
branch name. As a last resort, creating a fresh repo with a different name
but the same contents and then adding this in to the Terraform registry was
a helpful process as the UI revealed the error states.

https://developer.hashicorp.com/terraform/registry/providers/publishing


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
