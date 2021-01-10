package stile

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"

	"github.com/buildkite/go-buildkite/v2/buildkite"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

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

func getBuildkiteArtifact(artifactName string, buildNumber string, pipeline string, org string, diags diag.Diagnostics) (io.Reader, diag.Diagnostics) {
	apiToken, present := os.LookupEnv("BUILDKITE_READ_API_TOKEN")

	if present == false {
		diags = append(diags, diag.Diagnostic{
			Severity: diag.Error,
			Summary:  "Unable to find BUILDKITE_READ_API_TOKEN environment variable.",
			Detail:   "BUILDKITE_READ_API_TOKEN not present in environment",
		})

		return nil, diags
	}

	config, err := buildkite.NewTokenConfig(apiToken, true)

	if err != nil {
		log.Fatalf("client config failed: %s", err)

		diags = append(diags, diag.Diagnostic{
			Severity: diag.Error,
			Summary:  "Unable to configure Buildkite Client with BUILDKITE_READ_API_TOKEN.",
			Detail:   fmt.Sprintf("client config failed: %s", err),
		})

		return nil, diags
	}

	client := buildkite.NewClient(config.Client())

	artifacts, _, err := client.Artifacts.ListByBuild(org, pipeline, buildNumber, nil)

	if err != nil {
		diags = append(diags, diag.Diagnostic{
			Severity: diag.Error,
			Summary:  "Unable to find list buildkite artifacts.",
			Detail:   fmt.Sprintf("Attempted to read BFP Build %s", buildNumber),
		})

		log.Fatalf("list artifacts failed: %s", err)

		return nil, diags
	}

	for _, artifact := range artifacts {
		if artifactName == "" {
			data, err := json.MarshalIndent(artifact, "", "\t")

			if err != nil {
				log.Fatalf("json encode failed: %s", err)
			}

			fmt.Fprintf(os.Stdout, "%s\n", string(data))
		} else if artifactName == *artifact.Filename || artifactName == *artifact.ID {
			var buf bytes.Buffer
			_, err := client.Artifacts.DownloadArtifactByURL(*artifact.DownloadURL, &buf)
			if err != nil {
				diags = append(diags, diag.Diagnostic{
					Severity: diag.Error,
					Summary:  "Unable to find list buildkite artifacts.",
					Detail:   fmt.Sprintf("DownloadArtifactByURL failed: %s", err),
				})

				log.Fatalf("DownloadArtifactByURL failed: %s", err)

				return &buf, diags
			}

			return &buf, diags
		}
	}

	return nil, diags
}

func dataStileManifestRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	// Warning or errors can be collected in a slice type
	var diags diag.Diagnostics

	artifact, diags := getBuildkiteArtifact(d.Get("manifest_name").(string), strconv.Itoa(d.Get("bfp_build_number").(int)), "big-friendly-pipeline", "stile-education", diags)
	var buf bytes.Buffer
	tee := io.TeeReader(artifact, &buf)

	if diags != nil {
		return diags
	}

	manifest := map[string]interface{}{}
	err := json.NewDecoder(artifact).Decode(&manifest)
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
		log.Fatal(err)
	}

	sum := h.Sum(nil)

	d.SetId(fmt.Sprintf("%x", sum))

	return diags
}
