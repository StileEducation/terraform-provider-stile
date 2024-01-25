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
				Type:     schema.TypeString,
				Optional: true,
				Required: false,
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
			// This value is needed to keep terraform application's
			// idempotent. If a manifest becomes available after we've
			// applied the terraform then subsequent applications of
			// the terraform would say there is a diff when we don't
			// want them to.
			"used_fallback_manifest": &schema.Schema{
				Type:     schema.TypeBool,
				Computed: true,
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
				detail: fmt.Sprintf(
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
						summary: fmt.Sprintf("Unable to download artifact at URL %s", err),
						detail:  fmt.Sprintf("DownloadArtifactByURL failed: %s\nAre you on the VPN?", err),
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
	return nil, nil
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

	manifestName := d.Get("manifest_name").(string)
	bfpBuildNumber := strconv.Itoa(d.Get("bfp_build_number").(int))
	org := "stile-education"
	pipeline := "big-friendly-pipeline"

	var artifact io.Reader

	// Using `GetChange`, rather than the usual `Get`, is needed
	// because data resources don't get given the terraform state in
	// the `ResourceData` struct they're called with. This means that
	// `used_fallback_manifest` would always be false. They are,
	// however, given the terraform diff which we can get the
	// `used_fallback_manifest` value from if in a previous run we used
	// the fallback manifest.
	_, usedFallbackManifest := d.GetChange("used_fallback_manifest")

	// Only get the manifest artifact from buildkite if we haven't
	// previously used the fallback manifest. It would just be a
	// thrown away next anyway.
	if !usedFallbackManifest.(bool) {
		var err error
		artifact, err = getBuildkiteArtifact(apiToken, manifestName, bfpBuildNumber, pipeline, org)

		// Do our best to give a structured diagnostic if it's one of our
		// errors. If it's just been bubbled up from a library just put it
		// all in the summary.
		var diagError diagnosticError
		if errors.As(err, &diagError) {
			diags = append(diags, diag.Diagnostic{
				Severity: diag.Error,
				Summary:  diagError.summary,
				Detail:   diagError.detail,
			})
		} else if err != nil {
			diags = append(diags, diag.Diagnostic{
				Severity: diag.Error,
				Summary:  fmt.Sprintf("Failed to get Buildkite artifact: %v", err),
			})
			return diags
		}
	}

	// This will be nil in two cases:
	//
	// 1. On the first application if we can't find the manifest
	// 2. On subsequent applications if we used the fallback manifest last time.
	if artifact == nil {
		if fallbackArtifact, ok := d.GetOk("fallback_manifest"); ok {
			if noFallback, ok := os.LookupEnv("STILE_MANIFEST_NO_FALLBACK"); ok {
				noFallback, err := strconv.ParseBool(noFallback)
				if err != nil {
					diags = append(diags, diag.Diagnostic{
						Severity: diag.Error,
						Summary:  "Invalid valid for environment variable STILE_MANIFEST_NO_FALLBACK",
						Detail:   fmt.Sprintf("This value is used to determine whether you having a fallback manifest is allowed. It must be a valid boolean value (e.g. 0, 1, true, false, etc.): %v", err),
					})
					return diags
				}
				if noFallback {
					diags = append(diags, diag.Diagnostic{
						Severity: diag.Error,
						Summary:  fmt.Sprintf("Manifest %s not found for build %s in %s/%s", manifestName, bfpBuildNumber, org, pipeline),
						Detail:   "This may be beause the build failed or it is on a branch that does not build the manifest. You can use fallback_manifest to specify a map of the manifest that should be used if the expected one does not exist. A fallback was specified via fallback_manifest but fallback was disabled via the STILE_MANIFEST_NO_FALLBACK environment variable.",
					})
					return diags
				}
				// If we haven't disabled fallback, just warn that
				// we're falling back.
				diags = append(diags, diag.Diagnostic{
					Severity: diag.Warning,
					Summary:  fmt.Sprintf("Manifest %s not found for build %s in %s/%s, using fallback", manifestName, bfpBuildNumber, org, pipeline),
					Detail:   "This may be beause the build failed or it is on a branch that does not build the manifest. You can use fallback_manifest to specify a map of the manifest that should be used if the expected one does not exist. However, a fallback was specifie.",
				})
			}

			var buf bytes.Buffer
			if _, err := buf.WriteString(fallbackArtifact.(string)); err != nil {
				diags = append(diags, diag.FromErr(err)...)
			}
			artifact = &buf
			d.Set("used_fallback_manifest", true)
		} else {
			diags = append(diags, diag.Diagnostic{
				Severity: diag.Error,
				Summary:  fmt.Sprintf("Manifest %s not found for build %s in %s/%s", manifestName, bfpBuildNumber, org, pipeline),
				Detail:   "This may be beause the build failed or it is on a branch that does not build the manifest. You can use fallback_manifest to specify a map of the manifest that should be used if the expected one does not exist.",
			})
			return diags
		}
	}

	var buf bytes.Buffer
	tee := io.TeeReader(artifact, &buf)

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
		return diag.FromErr(err)
	}

	sum := h.Sum(nil)

	d.SetId(fmt.Sprintf("%x", sum))

	return diags
}
