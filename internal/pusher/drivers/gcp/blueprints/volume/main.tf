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

variable "capacity_gb" {
  type = number
}

variable "tier" {
  type    = string
  default = "BASIC_HDD"
}

variable "location" {
  type = string
}

variable "share" {
  type = string
}

variable "network" {
  type    = string
  default = "default"
}

variable "hash" {
  type    = string
  default = ""
}

provider "google" {
  project = var.project
  region  = var.region
}

resource "google_filestore_instance" "this" {
  name     = var.name
  location = var.location
  tier     = var.tier

  labels = var.hash == "" ? {} : {
    "guardian-hash" = var.hash
  }

  file_shares {
    name        = var.share
    capacity_gb = var.capacity_gb
  }

  networks {
    network = var.network
    modes   = ["MODE_IPV4"]
  }
}

output "volume_id" {
  value = google_filestore_instance.this.name
}

output "ip" {
  value = google_filestore_instance.this.networks[0].ip_addresses[0]
}

output "share" {
  value = google_filestore_instance.this.file_shares[0].name
}

output "mount" {
  value = "${google_filestore_instance.this.networks[0].ip_addresses[0]}:/${google_filestore_instance.this.file_shares[0].name}"
}

output "type" {
  value = "filestore"
}

output "guardian_hash" {
  value = var.hash
}
