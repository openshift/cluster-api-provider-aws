variable "cluster_name" {
  type    = "string"
  default = "my-cluster"
}

variable "master_count" {
  default = "1"
  type    = "string"
}

variable "ignition_masters" {
  type    = "list"
  default = []
}

variable "libvirt_worker_ips" {
  type    = "list"
  default = ["192.168.10.21", "192.168.10.22", "192.168.10.23"]
}

variable "libvirt_master_ips" {
  type    = "list"
  default = ["192.168.0.20"]
}

variable "libvirt_network_if" {
  type        = "string"
  description = "The name of the bridge to use"
  default     = "vrbr0"
}

variable "worker_count" {
  type    = "string"
  default = "3"
}

variable "libvirt_bootstrap_ip" {
  type    = "string"
  default = "192.168.0.1"
}
