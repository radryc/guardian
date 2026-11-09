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

variable "bucket" {
  type = string
}

variable "versioning" {
  type    = bool
  default = false
}

variable "force_destroy" {
  type    = bool
  default = false
}

variable "hash" {
  type    = string
  default = ""
}

provider "google" {
  project = var.project
  region  = var.region
}

resource "google_storage_bucket" "this" {
  name                        = var.bucket
  location                    = var.region
  force_destroy               = var.force_destroy
  uniform_bucket_level_access = true

  labels = var.hash == "" ? {} : {
    "guardian-hash" = var.hash
  }

  dynamic "versioning" {
    for_each = var.versioning ? [1] : []
    content {
      enabled = true
    }
  }
}

output "bucket" {
  value = google_storage_bucket.this.name
}

output "region" {
  value = google_storage_bucket.this.location
}

output "type" {
  value = "gcs"
}

output "guardian_hash" {
  value = var.hash
}
