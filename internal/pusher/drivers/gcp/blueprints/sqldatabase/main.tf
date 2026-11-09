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

variable "engine" {
  type = string
}

variable "database_version" {
  type = string
}

variable "tier" {
  type    = string
  default = "db-f1-micro"
}

variable "database" {
  type = string
}

variable "user" {
  type = string
}

variable "port" {
  type = number
}

variable "scheme" {
  type    = string
  default = "postgres"
}

variable "network" {
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

resource "google_sql_database_instance" "this" {
  name                = var.name
  region              = var.region
  database_version    = var.database_version
  deletion_protection = false

  settings {
    tier = var.tier

    user_labels = var.hash == "" ? {} : {
      "guardian-hash" = var.hash
    }

    ip_configuration {
      ipv4_enabled    = var.network == "" ? true : false
      private_network = var.network == "" ? null : var.network
    }
  }
}

resource "google_sql_database" "this" {
  name     = var.database
  instance = google_sql_database_instance.this.name
}

output "id" {
  value = google_sql_database_instance.this.name
}

output "engine" {
  value = var.engine
}

output "database" {
  value = google_sql_database.this.name
}

output "user" {
  value = var.user
}

output "port" {
  value = var.port
}

output "connection_name" {
  value = google_sql_database_instance.this.connection_name
}

output "public_ip" {
  value = try(google_sql_database_instance.this.public_ip_address, "")
}

output "private_ip" {
  value = try(google_sql_database_instance.this.private_ip_address, "")
}

output "endpoint" {
  value = try(coalesce(google_sql_database_instance.this.private_ip_address, google_sql_database_instance.this.public_ip_address), "")
}

output "url" {
  value = format("%s://%s@%s:%d/%s", var.scheme, var.user, try(coalesce(google_sql_database_instance.this.private_ip_address, google_sql_database_instance.this.public_ip_address), ""), var.port, google_sql_database.this.name)
}

output "type" {
  value = "cloudsql"
}

output "guardian_hash" {
  value = var.hash
}
