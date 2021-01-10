terraform {
  required_providers {
    stile = {
      versions = ["0.2"]
      source = "hashicorp.com/edu/stile"
    }
  }
}

data "stile_manifest" "all" {
  bfp_build_number = 470011
  manifest_name = "untested-excel-exporter-manifest.json"
}

# Returns all coffees
output "all" {
  value = data.stile_manifest.all
}

output "excel_exporter_image" {
  value = data.stile_manifest.all.service_versions["stile-excel-exporter-docker-image"]
}