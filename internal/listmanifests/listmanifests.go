// Package listmanifests implements the `terraform-provider-stile
// list-manifests <tf-dir>` subcommand.
//
// The subcommand walks a Terraform configuration directory, finds every
// `module "X" { source = ".../stile_manifest" ... }` block, evaluates
// each block's `manifest_name` argument, and emits a JSON list of the
// resolved manifest names on stdout.
//
// It is intentionally a focused walker — not a full Terraform
// evaluator. It resolves literal strings, same-module `local.*`
// references, and `var.*` references that have a literal `default`. It
// does NOT evaluate data sources, provision resources, talk to any
// network, or run any provider plugin code.
//
// Manifest names whose adjacent module block has a `fallback_manifest`
// argument set are still reported but flagged with `has_fallback:
// true`. Callers (e.g., stops) are expected to apply policy: a
// manifest with a fallback need not block a deploy because the TF
// fallback machinery covers the case where the artifact is absent.
//
// Output schema (stdout):
//
//	{
//	  "schema_version": 1,
//	  "manifests": [
//	    {
//	      "name": "untested-foo-manifest",
//	      "source_file": "main.tf",
//	      "source_line": 42,
//	      "has_fallback": false
//	    }
//	  ],
//	  "warnings": [...]
//	}
//
// Exit codes:
//
//	0 — success.
//	2 — invalid arguments / directory doesn't exist / not a TF dir.
//	3 — TF source unparseable (syntax error).
package listmanifests

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// SchemaVersion is the JSON output schema version. Incremented on
// breaking changes to the output shape.
const SchemaVersion = 1

// Manifest is one resolved `manifest_name` reference.
type Manifest struct {
	Name        string `json:"name"`
	SourceFile  string `json:"source_file"`
	SourceLine  int    `json:"source_line"`
	HasFallback bool   `json:"has_fallback"`
}

// Output is the top-level JSON shape emitted on stdout.
type Output struct {
	SchemaVersion int        `json:"schema_version"`
	Manifests     []Manifest `json:"manifests"`
	Warnings      []string   `json:"warnings"`
}

// Run is the subcommand entry point. Returns the exit code.
func Run(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: terraform-provider-stile list-manifests <tf-dir>")
		return 2
	}
	dir := args[0]

	info, err := os.Stat(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "list-manifests: cannot stat %q: %v\n", dir, err)
		return 2
	}
	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "list-manifests: %q is not a directory\n", dir)
		return 2
	}

	out, exit := walk(dir)
	if exit != 0 {
		return exit
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "list-manifests: failed to write JSON: %v\n", err)
		return 1
	}
	return 0
}

// walk discovers manifests starting from rootDir, recursing through
// `module "X" { source = "<relative-path>" }` references.
func walk(rootDir string) (*Output, int) {
	out := &Output{
		SchemaVersion: SchemaVersion,
		Manifests:     []Manifest{},
		Warnings:      []string{},
	}

	visited := make(map[string]bool)
	manifestSet := make(map[string]Manifest) // dedup by name

	var visit func(dir string) (fatal bool)
	visit = func(dir string) bool {
		abs, err := filepath.Abs(dir)
		if err != nil {
			out.Warnings = append(out.Warnings, fmt.Sprintf("cannot resolve %q: %v", dir, err))
			return false
		}
		if visited[abs] {
			return false
		}
		visited[abs] = true

		mod, parseErr := loadModule(abs)
		if parseErr != nil {
			// Syntax errors are fatal for the root module; treat as warnings
			// for nested modules so that one bad sub-module doesn't kill the
			// whole scan. The caller (stops) can decide based on the warnings.
			if abs == mustAbs(rootDir) {
				fmt.Fprintf(os.Stderr, "list-manifests: parse error in %s: %v\n", abs, parseErr)
				return true
			}
			out.Warnings = append(out.Warnings, fmt.Sprintf("parse error in %s: %v", abs, parseErr))
			return false
		}

		evalCtx := &hcl.EvalContext{
			Variables: map[string]cty.Value{
				"local": objectOrEmpty(mod.locals),
				"var":   objectOrEmpty(mod.vars),
			},
		}

		for _, mb := range mod.moduleBlocks {
			source, ok := evalString(mb.source, evalCtx)
			if !ok {
				out.Warnings = append(out.Warnings, fmt.Sprintf(
					"%s:%d: module %q has unresolvable `source` argument; skipping",
					relTo(rootDir, mb.file), mb.line, mb.label,
				))
				continue
			}

			// Order matters: a `module "manifest" { source = "../../modules/stile_manifest" }`
			// block is BOTH a local-path source AND the stile_manifest module
			// we want to extract from. Check for stile_manifest first; only
			// recurse into other local modules.
			if !isStileManifestSource(source) {
				if isLocalModuleSource(source) {
					next := filepath.Join(filepath.Dir(mb.file), source)
					visit(next)
				}
				// Non-local, non-stile_manifest sources (registry, git, etc.)
				// — nothing to extract or recurse into.
				continue
			}

			nameExpr, ok := mb.attrs["manifest_name"]
			if !ok {
				out.Warnings = append(out.Warnings, fmt.Sprintf(
					"%s:%d: stile_manifest module %q has no `manifest_name` argument",
					relTo(rootDir, mb.file), mb.line, mb.label,
				))
				continue
			}

			name, ok := evalString(nameExpr, evalCtx)
			if !ok {
				out.Warnings = append(out.Warnings, fmt.Sprintf(
					"%s:%d: stile_manifest module %q has unresolvable `manifest_name`; treat as unknown",
					relTo(rootDir, mb.file), mb.line, mb.label,
				))
				continue
			}

			_, hasFallback := mb.attrs["fallback_manifest"]

			// Use first occurrence's source location; later duplicates are
			// noted but not overridden.
			if existing, dup := manifestSet[name]; dup {
				_ = existing
				continue
			}
			manifestSet[name] = Manifest{
				Name:        name,
				SourceFile:  relTo(rootDir, mb.file),
				SourceLine:  mb.line,
				HasFallback: hasFallback,
			}
		}

		return false
	}

	if visit(rootDir) {
		return out, 3
	}

	// Collect, sorted by name for stable output.
	names := make([]string, 0, len(manifestSet))
	for n := range manifestSet {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		out.Manifests = append(out.Manifests, manifestSet[n])
	}

	return out, 0
}

// moduleData is the resolved local scope for one Terraform module
// (one directory of *.tf files).
type moduleData struct {
	locals       map[string]cty.Value
	vars         map[string]cty.Value
	moduleBlocks []moduleBlockRef
}

// moduleBlockRef is a `module "X" { ... }` block in some *.tf file.
type moduleBlockRef struct {
	label  string                       // X
	source hcl.Expression               // the `source` attribute expression
	attrs  map[string]hcl.Expression    // all attributes including manifest_name and fallback_manifest
	file   string                       // absolute path to the *.tf file
	line   int                          // line of the `module` keyword
}

// loadModule reads every *.tf file in dir, collects locals and
// variable defaults, and indexes module blocks for later inspection.
// It does NOT recurse into other modules.
func loadModule(dir string) (*moduleData, error) {
	mod := &moduleData{
		locals:       make(map[string]cty.Value),
		vars:         make(map[string]cty.Value),
		moduleBlocks: nil,
	}

	matches, err := filepath.Glob(filepath.Join(dir, "*.tf"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)

	for _, path := range matches {
		body, err := parseFile(path)
		if err != nil {
			// Surface as warning at the caller; treat partial modules as best-effort.
			return nil, err
		}

		for _, block := range body.Blocks {
			switch block.Type {
			case "locals":
				for name, attr := range block.Body.Attributes {
					val, diags := attr.Expr.Value(nil)
					if diags.HasErrors() {
						continue
					}
					if !val.IsKnown() || val.IsNull() {
						continue
					}
					if val.Type() == cty.String {
						mod.locals[name] = val
					}
				}
			case "variable":
				if len(block.Labels) == 0 {
					continue
				}
				varName := block.Labels[0]
				defAttr, ok := block.Body.Attributes["default"]
				if !ok {
					continue
				}
				val, diags := defAttr.Expr.Value(nil)
				if diags.HasErrors() {
					continue
				}
				if !val.IsKnown() || val.IsNull() {
					continue
				}
				if val.Type() == cty.String {
					mod.vars[varName] = val
				}
			case "module":
				if len(block.Labels) == 0 {
					continue
				}
				attrs := make(map[string]hcl.Expression, len(block.Body.Attributes))
				for name, a := range block.Body.Attributes {
					attrs[name] = a.Expr
				}
				sourceAttr, ok := block.Body.Attributes["source"]
				if !ok {
					continue // module without source — invalid, skip
				}
				mod.moduleBlocks = append(mod.moduleBlocks, moduleBlockRef{
					label:  block.Labels[0],
					source: sourceAttr.Expr,
					attrs:  attrs,
					file:   path,
					line:   block.DefRange().Start.Line,
				})
			}
		}
	}

	return mod, nil
}

func parseFile(path string) (*hclsyntax.Body, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	f, diags := hclsyntax.ParseConfig(src, path, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parsing %s: %s", path, diags.Error())
	}
	body, ok := f.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("parsing %s: unexpected body type", path)
	}
	return body, nil
}

// evalString attempts to evaluate expr against ctx as a string. Returns
// (value, true) on success or ("", false) if the result is unknown,
// not a string, or evaluation produced errors.
func evalString(expr hcl.Expression, ctx *hcl.EvalContext) (string, bool) {
	val, diags := expr.Value(ctx)
	if diags.HasErrors() {
		return "", false
	}
	if !val.IsKnown() || val.IsNull() {
		return "", false
	}
	if val.Type() != cty.String {
		return "", false
	}
	return val.AsString(), true
}

// objectOrEmpty returns an HCL-friendly object value. cty.ObjectVal
// panics on an empty map; this wrapper avoids that.
func objectOrEmpty(m map[string]cty.Value) cty.Value {
	if len(m) == 0 {
		return cty.EmptyObjectVal
	}
	return cty.ObjectVal(m)
}

// isLocalModuleSource reports whether a `source` value is a relative
// path to another local module (which the walker should recurse into).
func isLocalModuleSource(source string) bool {
	return strings.HasPrefix(source, "./") || strings.HasPrefix(source, "../")
}

// isStileManifestSource reports whether a `source` value points at the
// stile_manifest module (which the walker should extract manifests
// from). Convention in this codebase: every reference is a relative
// path ending in `/stile_manifest`. Remote sources (registry, git) are
// not considered — even if they happened to end in `stile_manifest`,
// we can't be sure they're THE module without resolving them.
func isStileManifestSource(source string) bool {
	if !isLocalModuleSource(source) {
		return false
	}
	clean := strings.TrimRight(source, "/")
	return filepath.Base(clean) == "stile_manifest"
}

// relTo returns p relative to base if possible, else p itself.
func relTo(base, p string) string {
	rel, err := filepath.Rel(mustAbs(base), p)
	if err != nil {
		return p
	}
	return rel
}

func mustAbs(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}
