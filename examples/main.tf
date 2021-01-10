terraform {
  required_providers {
    hashicups = {
      versions = ["0.2"]
      source = "hashicorp.com/edu/hashicups"
    }
  }
}

provider "hashicups" {}

module "psl" {
  source = "./stile_manifest"
}

output "psl" {
  value = module.psl.all
}

output "excel_exporter_image" {
  value = module.psl.excel_exporter_image
}
