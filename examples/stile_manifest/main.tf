terraform {
  required_providers {
    stile = {
      version = "0.2"
      source = "hashicorp.com/edu/stile"
    }
  }
}

data "stile_manifest" "all" {
  bfp_build_number = 926993
  manifest_name = "untested-prober-service-manifest.json"
  fallback_manifest = jsonencode({
    "name": "926993",
    "commits": {
      "git@github.com:StileEducation/dev-environment.git": "a commit"
    },
    "service_versions": {
      "stile-prober": "stile-prober",
      "stile-prometheus-node-exporter": "stile-prometheus-node-exporter",
      "prober-service": "prober-service",
      "stile-consul": "stile-consul",
      "stile-container-monitor": "stile-container-monitor",
      "registrator-docker-image": "registrator-docker-image"
    },
    "amis": {
      "base-ami": "base-iamge",
      "base-ami:ap-southeast-2": "base-ami:ap-southeast-2",
      "base-ami:us-west-2": "base-ami:us-west-2"
    }
  })
}

# Returns all coffees
output "all" {
  value = data.stile_manifest.all
}

output "image" {
  value = data.stile_manifest.all.service_versions["stile-prober"]
}
