terraform {
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = ">= 6.0"
    }
  }
}

variable "project" {
  type = string
}

variable "region" {
  type = string
}

variable "name" {
  type = string
}

variable "image" {
  type = string
}

variable "replicas" {
  type    = number
  default = 0
}

variable "env" {
  type    = map(string)
  default = {}
}

variable "args" {
  type    = list(string)
  default = []
}

variable "command" {
  type    = list(string)
  default = []
}

variable "port" {
  type    = number
  default = 8080
}

variable "cpu" {
  type    = string
  default = ""
}

variable "memory" {
  type    = string
  default = ""
}

variable "network" {
  type    = string
  default = ""
}

variable "subnet" {
  type    = string
  default = ""
}

variable "hash" {
  type    = string
  default = ""
}

variable "public" {
  type    = bool
  default = true
}

provider "google" {
  project = var.project
  region  = var.region
}

resource "google_cloud_run_v2_service" "this" {
  name                = var.name
  location            = var.region
  ingress             = "INGRESS_TRAFFIC_ALL"
  deletion_protection = false

  labels = var.hash == "" ? {} : {
    "guardian-hash" = var.hash
  }

  template {
    dynamic "scaling" {
      for_each = var.replicas > 0 ? [1] : []
      content {
        min_instance_count = var.replicas
        max_instance_count = var.replicas
      }
    }

    dynamic "vpc_access" {
      for_each = var.network == "" ? [] : [1]
      content {
        egress = "PRIVATE_RANGES_ONLY"

        network_interfaces {
          network    = var.network
          subnetwork = var.subnet
        }
      }
    }

    containers {
      image   = var.image
      args    = var.args
      command = var.command

      ports {
        container_port = var.port
      }

      dynamic "env" {
        for_each = var.env
        content {
          name  = env.key
          value = env.value
        }
      }

      dynamic "resources" {
        for_each = var.cpu == "" && var.memory == "" ? [] : [1]
        content {
          limits = merge(
            var.cpu == "" ? {} : { cpu = var.cpu },
            var.memory == "" ? {} : { memory = var.memory },
          )
        }
      }
    }
  }
}

resource "google_cloud_run_v2_service_iam_member" "public" {
  count = var.public ? 1 : 0

  project  = var.project
  location = google_cloud_run_v2_service.this.location
  name     = google_cloud_run_v2_service.this.name
  role     = "roles/run.invoker"
  member   = "allUsers"
}

output "service" {
  value = google_cloud_run_v2_service.this.name
}

output "url" {
  value = google_cloud_run_v2_service.this.uri
}

output "region" {
  value = google_cloud_run_v2_service.this.location
}

output "type" {
  value = "cloudrun"
}

output "guardian_hash" {
  value = var.hash
}
