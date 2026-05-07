package listmanifests

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// writeFiles materialises a map of relative-path -> content into a fresh
// temp directory and returns the directory path.
func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for relPath, content := range files {
		full := filepath.Join(dir, relPath)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", full, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
	return dir
}

func manifestNames(out *Output) []string {
	names := make([]string, 0, len(out.Manifests))
	for _, m := range out.Manifests {
		names = append(names, m.Name)
	}
	sort.Strings(names)
	return names
}

func runWalk(t *testing.T, dir string) *Output {
	t.Helper()
	out, exit := walk(dir)
	if exit != 0 {
		t.Fatalf("walk(%s) exit=%d, expected 0; warnings=%v", dir, exit, out.Warnings)
	}
	return out
}

func TestLiteralOnly(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"main.tf": `
module "manifest" {
  source        = "../../modules/stile_manifest"
  manifest_name = "untested-foo-manifest"
}
`,
		"../../modules/stile_manifest/main.tf": `
data "stile_manifest" "all" {}
`,
	})

	out := runWalk(t, dir)
	if got, want := manifestNames(out), []string{"untested-foo-manifest"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got manifests=%v want=%v warnings=%v", got, want, out.Warnings)
	}
	if out.Manifests[0].HasFallback {
		t.Errorf("expected has_fallback=false, got true")
	}
}

func TestSameModuleLocalRef(t *testing.T) {
	// The certbot/dev-fargate pattern: manifest_name = local.foo where
	// local.foo is defined in a `locals { ... }` block in the same file.
	dir := writeFiles(t, map[string]string{
		"main.tf": `
locals {
  manifest_name = "untested-certbot-dev-manifest"
}

module "manifest" {
  source        = "../../modules/stile_manifest"
  manifest_name = local.manifest_name
}
`,
		"../../modules/stile_manifest/main.tf": `data "stile_manifest" "all" {}`,
	})

	out := runWalk(t, dir)
	if got, want := manifestNames(out), []string{"untested-certbot-dev-manifest"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestLocalRefInDifferentFile(t *testing.T) {
	// Locals can be in any *.tf file in the same module.
	dir := writeFiles(t, map[string]string{
		"main.tf": `
module "manifest" {
  source        = "../../modules/stile_manifest"
  manifest_name = local.manifest_name
}
`,
		"locals.tf": `
locals {
  manifest_name = "untested-bar-manifest"
}
`,
		"../../modules/stile_manifest/main.tf": `data "stile_manifest" "all" {}`,
	})

	out := runWalk(t, dir)
	if got, want := manifestNames(out), []string{"untested-bar-manifest"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestVariableDefault(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"main.tf": `
variable "manifest_name" {
  default = "untested-baz-manifest"
}

module "manifest" {
  source        = "../../modules/stile_manifest"
  manifest_name = var.manifest_name
}
`,
		"../../modules/stile_manifest/main.tf": `data "stile_manifest" "all" {}`,
	})

	out := runWalk(t, dir)
	if got, want := manifestNames(out), []string{"untested-baz-manifest"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestModuleRecursion(t *testing.T) {
	// The SLB pattern: app-level main.tf calls a service module which
	// itself calls stile_manifest.
	dir := writeFiles(t, map[string]string{
		"main.tf": `
module "svc" {
  source = "../../modules/svc"
}
`,
		"../../modules/svc/main.tf": `
module "manifest" {
  source        = "../../modules/stile_manifest"
  manifest_name = "untested-svc-manifest"
}
`,
		"../../modules/stile_manifest/main.tf": `data "stile_manifest" "all" {}`,
	})

	out := runWalk(t, dir)
	if got, want := manifestNames(out), []string{"untested-svc-manifest"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestMultipleManifests(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"main.tf": `
module "manifest_a" {
  source        = "../../modules/stile_manifest"
  manifest_name = "untested-a-manifest"
}

module "manifest_b" {
  source        = "../../modules/stile_manifest"
  manifest_name = "untested-b-manifest"
}
`,
		"../../modules/stile_manifest/main.tf": `data "stile_manifest" "all" {}`,
	})

	out := runWalk(t, dir)
	if got, want := manifestNames(out), []string{"untested-a-manifest", "untested-b-manifest"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestFallbackManifestFlagged(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"main.tf": `
module "manifest_required" {
  source        = "../../modules/stile_manifest"
  manifest_name = "untested-required-manifest"
}

module "manifest_optional" {
  source            = "../../modules/stile_manifest"
  manifest_name     = "untested-optional-manifest"
  fallback_manifest = jsonencode({ images = {} })
}
`,
		"../../modules/stile_manifest/main.tf": `data "stile_manifest" "all" {}`,
	})

	out := runWalk(t, dir)
	if got, want := len(out.Manifests), 2; got != want {
		t.Fatalf("got %d manifests, want %d (warnings: %v)", got, want, out.Warnings)
	}

	for _, m := range out.Manifests {
		switch m.Name {
		case "untested-required-manifest":
			if m.HasFallback {
				t.Errorf("required manifest should not have HasFallback=true")
			}
		case "untested-optional-manifest":
			if !m.HasFallback {
				t.Errorf("optional manifest should have HasFallback=true")
			}
		default:
			t.Errorf("unexpected manifest %q", m.Name)
		}
	}
}

func TestNoManifestsReturnsEmpty(t *testing.T) {
	// Like an interactive — TF has no stile_manifest references at all.
	dir := writeFiles(t, map[string]string{
		"main.tf": `
resource "aws_s3_bucket" "foo" {
  bucket = "some-bucket"
}
`,
	})

	out := runWalk(t, dir)
	if len(out.Manifests) != 0 {
		t.Errorf("expected no manifests, got %v", manifestNames(out))
	}
	if len(out.Warnings) != 0 {
		t.Errorf("expected no warnings, got %v", out.Warnings)
	}
}

func TestUnresolvableVarReportsWarning(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"main.tf": `
variable "injected_manifest" {
  type = string
}

module "manifest" {
  source        = "../../modules/stile_manifest"
  manifest_name = var.injected_manifest
}
`,
		"../../modules/stile_manifest/main.tf": `data "stile_manifest" "all" {}`,
	})

	out := runWalk(t, dir)
	if len(out.Manifests) != 0 {
		t.Errorf("expected no manifests (var has no default), got %v", manifestNames(out))
	}
	if len(out.Warnings) == 0 {
		t.Errorf("expected at least one warning about unresolvable manifest_name")
	}
}

func TestRemoteSourceIgnored(t *testing.T) {
	// A `module "X" { source = "git::..." }` reference should be skipped
	// (we can't follow it) but should NOT be treated as a stile_manifest
	// module just because the path string ends in stile_manifest.
	dir := writeFiles(t, map[string]string{
		"main.tf": `
module "remote" {
  source        = "git::https://example.com/some/repo//stile_manifest"
  manifest_name = "untested-should-not-extract"
}
`,
	})

	out := runWalk(t, dir)
	if len(out.Manifests) != 0 {
		t.Errorf("remote `git::...` source should not be treated as a stile_manifest module; got %v", manifestNames(out))
	}
}

func TestDuplicateManifestNamesDeduped(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"main.tf": `
module "a" {
  source        = "../../modules/stile_manifest"
  manifest_name = "untested-shared"
}

module "b" {
  source        = "../../modules/stile_manifest"
  manifest_name = "untested-shared"
}
`,
		"../../modules/stile_manifest/main.tf": `data "stile_manifest" "all" {}`,
	})

	out := runWalk(t, dir)
	if got, want := manifestNames(out), []string{"untested-shared"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected deduplication; got %v", got)
	}
}

func TestNonexistentDirectoryExits2(t *testing.T) {
	exit := Run([]string{"/this/does/not/exist/xyz"})
	if exit != 2 {
		t.Errorf("expected exit 2, got %d", exit)
	}
}

func TestUsageRequiresExactlyOneArg(t *testing.T) {
	if exit := Run(nil); exit != 2 {
		t.Errorf("expected exit 2 with no args, got %d", exit)
	}
	if exit := Run([]string{"a", "b"}); exit != 2 {
		t.Errorf("expected exit 2 with too many args, got %d", exit)
	}
}

func TestSyntaxErrorInRootIsFatal(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"main.tf": `module "x" { this is not valid hcl }`,
	})
	_, exit := walk(dir)
	if exit != 3 {
		t.Errorf("expected exit 3 (parse error), got %d", exit)
	}
}
