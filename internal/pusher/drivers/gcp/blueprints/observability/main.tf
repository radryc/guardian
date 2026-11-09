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

variable "config" {
  type      = string
  sensitive = true
}

variable "env" {
  type    = map(string)
  default = {}
}

variable "port" {
  type    = number
  default = 4317
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
      image = var.image
      args  = ["--config=env:OTEL_CONFIG"]

      ports {
        container_port = var.port
      }

      dynamic "env" {
        for_each = merge({ OTEL_CONFIG = var.config }, var.env)
        content {
          name  = env.key
          value = env.value
        }
      }
    }
  }
}

resource "google_cloud_run_v2_service_iam_member" "public" {
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

output "endpoint" {
  value = "${replace(google_cloud_run_v2_service.this.uri, "https://", "")}:443"
}

output "region" {
  value = google_cloud_run_v2_service.this.location
}

output "type" {
  value = "gateway"
}

output "guardian_hash" {
  value = var.hash
}
