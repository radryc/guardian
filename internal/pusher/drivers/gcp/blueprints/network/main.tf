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

variable "subnet" {
  type = string
}

variable "cidr" {
  type = string
}

variable "internal" {
  type    = bool
  default = false
}

variable "private_services_access" {
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

resource "google_compute_network" "this" {
  name                    = var.name
  auto_create_subnetworks = false
  routing_mode            = "REGIONAL"
}

resource "google_compute_subnetwork" "this" {
  name                     = var.subnet
  ip_cidr_range            = var.cidr
  region                   = var.region
  network                  = google_compute_network.this.id
  private_ip_google_access = var.internal
}

# Private Services Access lets Cloud SQL (and other managed services) attach a
# private IP range to this network. One connection per network.
resource "google_compute_global_address" "private_services" {
  count = var.private_services_access ? 1 : 0

  name          = "${var.name}-psa"
  purpose       = "VPC_PEERING"
  address_type  = "INTERNAL"
  prefix_length = 16
  network       = google_compute_network.this.id
}

resource "google_service_networking_connection" "private_services" {
  count = var.private_services_access ? 1 : 0

  network                 = google_compute_network.this.id
  service                 = "servicenetworking.googleapis.com"
  reserved_peering_ranges = [google_compute_global_address.private_services[0].name]

  depends_on = [google_compute_global_address.private_services]
}

output "network_id" {
  value = google_compute_network.this.id
}

output "network_name" {
  value = google_compute_network.this.name
}

output "subnet_id" {
  value = google_compute_subnetwork.this.id
}

output "subnet_name" {
  value = google_compute_subnetwork.this.name
}

output "cidr" {
  value = google_compute_subnetwork.this.ip_cidr_range
}

output "region" {
  value = var.region
}

output "type" {
  value = "vpc"
}

output "guardian_hash" {
  value = var.hash
}
