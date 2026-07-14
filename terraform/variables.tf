variable "region" {
  type    = string
  default = "eu-west-2" # London — data residency for a UK bank
}

variable "environment" {
  type    = string
  default = "prod"
}

variable "cluster_name" {
  type    = string
  default = "bank-platform"
}

variable "kubernetes_version" {
  type    = string
  default = "1.30"
}

variable "vpc_cidr" {
  type    = string
  default = "10.20.0.0/16"
}

variable "db_instance_class" {
  type    = string
  default = "db.t4g.medium"
}

variable "db_name" {
  type    = string
  default = "accounts"
}

variable "db_username" {
  type    = string
  default = "accounts"
}

variable "app_namespace" {
  type    = string
  default = "accounts"
}

variable "app_service_account" {
  type    = string
  default = "accounts-accounts" # {release}-accounts, matches the Helm SA name
}

variable "app_consumer_service_account" {
  type    = string
  default = "accounts-accounts-consumer" # consumer Helm SA name
}
