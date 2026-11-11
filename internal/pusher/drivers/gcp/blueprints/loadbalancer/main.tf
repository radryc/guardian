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

variable "targets" {
  type = list(string)
}

variable "port" {
  type    = number
  default = 80
}

variable "hash" {
  type    = string
  default = ""
}

provider "google" {
  project = var.project
  region  = var.region
}

resource "google_compute_global_address" "this" {
  name = "${var.name}-ip"
}

resource "google_compute_region_network_endpoint_group" "target" {
  for_each = toset(var.targets)

  name                  = "${var.name}-${substr(sha1(each.key), 0, 8)}"
  region                = var.region
  network_endpoint_type = "SERVERLESS"

  cloud_run {
    service = each.key
  }
}

resource "google_compute_backend_service" "this" {
  name = "${var.name}-backend"

  dynamic "backend" {
    for_each = google_compute_region_network_endpoint_group.target
    content {
      group = backend.value.id
    }
  }
}

resource "google_compute_url_map" "this" {
  name            = "${var.name}-urlmap"
  default_service = google_compute_backend_service.this.id
}

resource "google_compute_target_http_proxy" "this" {
  name    = "${var.name}-proxy"
  url_map = google_compute_url_map.this.id
}

resource "google_compute_global_forwarding_rule" "this" {
  name       = "${var.name}-fwd"
  target     = google_compute_target_http_proxy.this.id
  port_range = tostring(var.port)
  ip_address = google_compute_global_address.this.id
}

output "lb_name" {
  value = var.name
}

output "lb_ip" {
  value = google_compute_global_address.this.address
}

output "dns_name" {
  value = google_compute_global_address.this.address
}

output "url" {
  value = var.port == 80 ? "http://${google_compute_global_address.this.address}" : "http://${google_compute_global_address.this.address}:${var.port}"
}

output "type" {
  value = "http-lb"
}

output "guardian_hash" {
  value = var.hash
}
