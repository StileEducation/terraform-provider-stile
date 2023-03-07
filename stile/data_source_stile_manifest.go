package stile

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"

	"github.com/buildkite/go-buildkite/v2/buildkite"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

type diagnosticError struct {
	summary string
	detail  string
}

func (e diagnosticError) Error() string {
	return fmt.Sprintf("%s: %v", e.summary, e.detail)
}

func dataStileManifest() *schema.Resource {
	return &schema.Resource{
		ReadContext: dataStileManifestRead,
		Schema: map[string]*schema.Schema{
			"manifest_name": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
				Optional: false,
				Computed: false,
			},
			"bfp_build_number": &schema.Schema{
				Type:     schema.TypeInt,
				Required: true,
				Optional: false,
				Computed: false,
			},
			"fallback_manifest": &schema.Schema{
				Type: schema.TypeMap,
				Optional: true,
				Computed: false,
				Elem: &schema.Schema {
					Type: schema.TypeString,
				},
			},
			"name": &schema.Schema{
				Type:     schema.TypeString,
				Computed: true,
			},
			"amis": &schema.Schema{
				Type:     schema.TypeMap,
				Computed: true,
			},
			"service_versions": &schema.Schema{
				Type:     schema.TypeMap,
				Computed: true,
				Elem: &schema.Schema{
					Type: schema.TypeString,
				},
			},
		},
	}
}

func getBuildkiteArtifact(apiToken string, artifactName string, buildNumber string, pipeline string, org string) (io.Reader, error) {
	config, err := buildkite.NewTokenConfig(apiToken, true)

	if err != nil {
		log.Printf("client config failed: %s", err)
		return nil, diagnosticError{
			summary: "Unable to configure Buildkite Client with BUILDKITE_READ_API_TOKEN",
			detail:  fmt.Sprintf("client config failed: %v", err),
		}
	}

	client := buildkite.NewClient(config.Client())

	// This is a pointer, so for ease of use we assign it with the default
	// values for the structure. If we used the, the perhaps more
	// idiomatic, `var` then `opts` would be nil and we'd have to special
	// case setting `Page` for the first iteration of the loop where it
	// would be `nil`
	opts := &buildkite.ArtifactListOptions{ListOptions: buildkite.ListOptions{}}

	// Buildkite's Artifacts API is paginated so we need to go through each
	// page until we find the artifact we're looking for, or run out of
	// pages.
	for {
		artifacts, response, err := client.Artifacts.ListByBuild(org, pipeline, buildNumber, opts)

		if err != nil {
			log.Printf("list artifacts failed: %s", err)
			return nil, diagnosticError{
				summary: fmt.Sprintf("Unable to list buildkite artifacts for build %s in pipeline %s/%s", buildNumber, org, pipeline),
				detail:  fmt.Sprintf(
					"This can mean the artifact does not exist or your Buildkite API token has insufficient permission to access it: %v",
					err,
				),
			}
		}

		for _, artifact := range artifacts {
			if artifactName == "" {
				data, err := json.MarshalIndent(artifact, "", "\t")
				if err != nil {
					log.Printf("json encode failed: %s", err)
					return nil, diagnosticError{
						summary: "Failed to encode artifact as JSON",
						detail:  err.Error(),
					}
				}
				fmt.Fprintf(os.Stdout, "%s\n", string(data))
			} else if artifactName == *artifact.Filename || artifactName == *artifact.ID {
				var buf bytes.Buffer
				_, err := client.Artifacts.DownloadArtifactByURL(*artifact.DownloadURL, &buf)
				if err != nil {
					log.Printf("DownloadArtifactByURL failed: %s", err)
					return nil, diagnosticError{
						summary: fmt.Sprintf("Unabled to download artifact at URL %s", err),
						detail:  fmt.Sprintf("DownloadArtifactByURL failed: %s", err),
					}
				}

				return &buf, nil
			}
		}

		// This indicates that there are no more pages to look at and
		// we haven't found the manifest we're looking for.
		if response.NextPage == 0 {
			break
		}

		opts.Page = response.NextPage
	}
	log.Printf("Could not find manifest %s for build number %s in %s/%s", artifactName, buildNumber, org, pipeline)
	return nil, diagnosticError{
		summary: "Could not find manifest",
		detail:  fmt.Sprintf("Could not find manifest %s for build number %s in %s/%s", artifactName, buildNumber, org, pipeline),
	}
}

func dataStileManifestRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	// Warning or errors can be collected in a slice type
	var diags diag.Diagnostics

	apiToken, present := os.LookupEnv("BUILDKITE_READ_API_TOKEN")

	if present == false {
		diags = append(diags, diag.Diagnostic{
			Severity: diag.Error,
			Summary:  "Unable to find BUILDKITE_READ_API_TOKEN environment variable.",
			Detail:   "BUILDKITE_READ_API_TOKEN not present in environment",
		})

		return diags
	}


	artifact, err := getBuildkiteArtifact(apiToken, d.Get("manifest_name").(string), strconv.Itoa(d.Get("bfp_build_number").(int)), "big-friendly-pipeline", "stile-education")

	// Do our best to give a structured diagnostic if it's one of our
	// errors. If it's just been bubbled up from a library just put it
	// all in the summary.
	var diagError diagnosticError
	if errors.As(err, &diagError) {
		diags = append(diags, diag.Diagnostic{
			Severity:      diag.Error,
			Summary:       diagError.summary,
			Detail:        diagError.detail,
		})
	} else if err != nil {
		diags = append(diags, diag.Diagnostic{
			Severity: diag.Error,
			Summary:  fmt.Sprintf("Failed to get Buildkite artifact: %v", err),
		})
		return diags
	}
	var buf bytes.Buffer
	tee := io.TeeReader(artifact, &buf)

	manifest := map[string]interface{}{}
	err = json.NewDecoder(artifact).Decode(&manifest)
	if err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("amis", manifest["amis"]); err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("bfp_build_number", d.Get("bfp_build_number").(int)); err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("manifest_name", d.Get("manifest_name")); err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("name", manifest["name"]); err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("service_versions", manifest["service_versions"]); err != nil {
		return diag.FromErr(err)
	}
	h := sha256.New()
	if _, err := io.Copy(h, tee); err != nil {
		return diag.FromErr(err)
	}

	sum := h.Sum(nil)

	d.SetId(fmt.Sprintf("%x", sum))

	return diags
}
