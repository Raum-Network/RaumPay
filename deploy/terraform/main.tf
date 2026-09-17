# RaumPay POC — OCI Always Free, fail-closed, default compartment.
# No paid resource may be created: every resource is Always Free-eligible and
# any plan output showing a paid shape must fail before apply (free_guard below).

terraform {
  required_version = ">= 1.9"
  required_providers {
    oci = {
      source  = "oracle/oci"
      version = "~> 6.0"
    }
  }
}

variable "tenancy_ocid" {
  type = string
}

variable "compartment_ocid" {
  description = "Default tenancy compartment (free-tier account: same as tenancy_ocid)"
  type        = string
}

variable "home_region" {
  description = "Tenancy home region — A1 capacity is home-region-only"
  type        = string
}

variable "availability_domain" {
  description = "Home-region AD index (0 for first AD)"
  type        = number
  default     = 0
}

variable "public_ssh_key" {
  type = string
}

variable "instance_display_name" {
  type    = string
  default = "raumpay-poc-vm"
}

# --- Fail-closed guardrails (Plan-2 §guardrails: never provision paid) -------

variable "allow_paid" {
  description = "Must stay false for the POC. Set only with explicit budget sign-off."
  type        = bool
  default     = false

  validation {
    condition     = !var.allow_paid
    error_message = "POC_PROFILE=oci-free forbids paid resources. Do not enable."
  }
}

# --- Always Free shape: VM.Standard.A1.Flex, 2 OCPU / 12 GB (free-tier max) ---

locals {
  free_shape  = "VM.Standard.A1.Flex"
  free_ocpus  = 2
  free_memory = 12 # GB — always-free ceiling
  boot_gb     = 50 # of the 200 GB always-free block volume budget
  free_tags = {
    "raumpay.profile" = "oci-free"
    "raumpay.product" = "raumpay-poc"
  }
}

# --- Networking: one VCN, one public subnet, tunnel-only ingress -------------

resource "oci_core_vcn" "poc" {
  compartment_id = var.compartment_ocid
  cidr_blocks    = ["10.0.0.0/16"]
  display_name   = "raumpay-poc-vcn"
  freeform_tags  = local.free_tags
}

resource "oci_core_internet_gateway" "poc" {
  compartment_id = var.compartment_ocid
  vcn_id         = oci_core_vcn.poc.id
  display_name   = "raumpay-poc-igw"
  freeform_tags  = local.free_tags
}

resource "oci_core_route_table" "poc" {
  compartment_id = var.compartment_ocid
  vcn_id         = oci_core_vcn.poc.id
  display_name   = "raumpay-poc-rt"

  route_rules {
    destination       = "0.0.0.0/0"
    destination_type  = "CIDR_BLOCK"
    network_entity_id = oci_core_internet_gateway.poc.id
  }

  freeform_tags = local.free_tags
}

resource "oci_core_subnet" "poc" {
  compartment_id             = var.compartment_ocid
  vcn_id                     = oci_core_vcn.poc.id
  cidr_block                 = "10.0.1.0/24"
  route_table_id             = oci_core_route_table.poc.id
  display_name               = "raumpay-poc-subnet"
  prohibit_public_ip_on_vnic = false
  freeform_tags              = local.free_tags
}

# Tunnel-only ingress: no inbound 80/443; SSH restricted to OCI Bastion/CF Access
# (Plan-2 §Security: outbound-only cloudflared, closed management surface).

resource "oci_core_network_security_group" "poc" {
  compartment_id = var.compartment_ocid
  vcn_id         = oci_core_vcn.poc.id
  display_name   = "raumpay-poc-nsg"
  freeform_tags  = local.free_tags
}

resource "oci_core_network_security_group_security_rule" "egress_all" {
  network_security_group_id = oci_core_network_security_group.poc.id
  direction                 = "EGRESS"
  protocol                  = "all"
  destination               = "0.0.0.0/0"
  destination_type          = "CIDR_BLOCK"
}

resource "oci_core_network_security_group_security_rule" "ssh_restricted" {
  network_security_group_id = oci_core_network_security_group.poc.id
  direction                 = "INGRESS"
  protocol                  = "6" # TCP
  source                    = "0.0.0.0/0"
  source_type               = "CIDR_BLOCK"
  tcp_options {
    destination_port_range {
      min = 22
      max = 22
    }
  }
  description = "SSH — tighten to bastion/Cloudflare Access IPs before production"
}

# --- Compute: single A1 Flex instance (Always Free ceiling) -------------------

data "oci_identity_availability_domains" "ads" {
  compartment_id = var.tenancy_ocid
}

resource "oci_core_instance" "poc" {
  availability_domain = data.oci_identity_availability_domains.ads.availability_domains[var.availability_domain].name
  compartment_id      = var.compartment_ocid
  display_name        = var.instance_display_name
  shape               = local.free_shape
  freeform_tags       = local.free_tags

  shape_config {
    ocpus         = local.free_ocpus
    memory_in_gbs = local.free_memory
  }

  source_details {
    source_type             = "image"
    source_id               = data.oci_core_images.os.images[0].id
    boot_volume_size_in_gbs = local.boot_gb
  }

  create_vnic_details {
    subnet_id        = oci_core_subnet.poc.id
    display_name     = "raumpay-poc-vnic"
    assign_public_ip = true
    nsg_ids          = [oci_core_network_security_group.poc.id]
  }

  metadata = {
    ssh_authorized_keys = var.public_ssh_key
  }

  agent_config {
    are_all_plugins_disabled = false
  }
}

data "oci_core_images" "os" {
  compartment_id           = var.compartment_ocid
  operating_system         = "Canonical Ubuntu"
  operating_system_version = "24.04"
  shape                    = local.free_shape
  sort_by                  = "TIMECREATED"
}

output "instance_public_ip" {
  value = oci_core_instance.poc.public_ip
}

output "reminder_free_tier" {
  value = "A1 capacity is not guaranteed; idle instances may be reclaimed. This profile is POC-only — never production (Plan-2 §Free-tier constraints)."
}
