provider "libvirt" {
  uri = "qemu:///system"
}

resource "libvirt_volume" "base_image" {
  name   = "base"
  source = "file:///vm/iso/rhcos-qemu.qcow2"
}

// Resources
resource "libvirt_ignition" "master" {
  count   = "${var.master_count}"
  name    = "master-${count.index}.ign"
  content = "${file(format("%s/%s", path.cwd, var.ignition_masters[count.index]))}"
}

module "bootstrap" {
  source = "github.com/openshift/installer//modules/libvirt/bootstrap"

  addresses      = ["192.168.0.1"]
  base_volume_id = "${libvirt_volume.base_image.id}"
  cluster_name   = "${var.cluster_name}"
  ignition       = "{\"ignition\": {\"version\": \"2.2.0\"}}"
  network_id     = "${libvirt_network.libvirt_net.id}"
}

resource "libvirt_volume" "master" {
  count          = "${var.master_count}"
  name           = "master${count.index}"
  base_volume_id = "${libvirt_volume.base_image.id}"
}

resource "libvirt_network" "libvirt_net" {
  name   = "dev-net"
  mode   = "nat"
  bridge = "${var.libvirt_network_if}"

  domain    = "aos-cloud.eu"
  addresses = ["192.168.0.0/24"]

  dns = [{
    local_only = true

    hosts = ["${flatten(list(
      data.libvirt_network_dns_host_template.bootstrap.*.rendered,
      data.libvirt_network_dns_host_template.masters.*.rendered,
      data.libvirt_network_dns_host_template.etcds.*.rendered,
      data.libvirt_network_dns_host_template.workers.*.rendered,
    ))}"]
  }]

  autostart = true
}

resource "libvirt_domain" "master" {
  count = "${var.master_count}"

  name = "master${count.index}"

  memory = "2048"
  vcpu   = "2"

  coreos_ignition = "${libvirt_ignition.master.*.id[count.index]}"

  disk {
    volume_id = "${element(libvirt_volume.master.*.id, count.index)}"
  }

  console {
    type        = "pty"
    target_port = 0
  }

  network_interface {
    network_id = "${libvirt_network.libvirt_net.id}"
    hostname   = "${var.cluster_name}-master-${count.index}"
    addresses  = ["${var.libvirt_master_ips[count.index]}"]
  }
}

locals {
  "hostnames" = [
    "${var.cluster_name}-api",
    "${var.cluster_name}-tnc",
  ]
}

data "libvirt_network_dns_host_template" "bootstrap" {
  count    = "${length(local.hostnames)}"
  ip       = "${var.libvirt_bootstrap_ip}"
  hostname = "${local.hostnames[count.index]}"
}

data "libvirt_network_dns_host_template" "masters" {
  count    = "${var.master_count * length(local.hostnames)}"
  ip       = "${var.libvirt_master_ips[count.index / length(local.hostnames)]}"
  hostname = "${local.hostnames[count.index % length(local.hostnames)]}"
}

data "libvirt_network_dns_host_template" "etcds" {
  count    = "${var.master_count}"
  ip       = "${var.libvirt_master_ips[count.index]}"
  hostname = "${var.cluster_name}-etcd-${count.index}"
}

data "libvirt_network_dns_host_template" "workers" {
  count    = "${var.worker_count}"
  ip       = "${var.libvirt_worker_ips[count.index]}"
  hostname = "${var.cluster_name}"
}
