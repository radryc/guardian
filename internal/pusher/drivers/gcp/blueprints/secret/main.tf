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

variable "secret_id" {
  type = string
}

variable "value" {
  type      = string
  sensitive = true
  default   = ""
}

variable "create_version" {
  type    = bool
  default = true
}

variable "hash" {
  type    = string
  default = ""
}

provider "google" {
  project = var.project
  region  = var.region
}

resource "google_secret_manager_secret" "this" {
  secret_id = var.secret_id
  labels = var.hash == "" ? {} : {
    "guardian-hash" = var.hash
  }

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_version" "this" {
  count = var.create_version ? 1 : 0

  secret      = google_secret_manager_secret.this.id
  secret_data = var.value

  lifecycle {
    create_before_destroy = true
  }
}

output "secret_id" {
  value = google_secret_manager_secret.this.secret_id
}

output "secret" {
  value = google_secret_manager_secret.this.secret_id
}

output "type" {
  value = "secretmanager"
}

output "guardian_hash" {
  value = var.hash
}
