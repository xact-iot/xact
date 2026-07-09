SHELL := /usr/bin/env bash

ANSIBLE_DIR := ansible
INVENTORY ?= inventory.ini
KUBECONFIG := k3s/kubeconfig
K3S_BIN := k3s/k3s
VAGRANT_HOME := $(CURDIR)/.vagrant.d
VAGRANT := VAGRANT_HOME=$(VAGRANT_HOME) vagrant
KUBECTL := $(shell command -v kubectl 2>/dev/null || true)
ANSIBLE := ansible -i $(INVENTORY)
ANSIBLE_PLAYBOOK := ansible-playbook -i $(INVENTORY)

.PHONY: help up status ssh-node1 ping prepare install verify kubeconfig k3s-bin nodes pods local-nodes local-pods down destroy clean

help:
	@printf '%s\n' "K3s HA lab targets"
	@printf '%s\n' ""
	@printf '%s\n' "  make up          Create/start the three VirtualBox VMs"
	@printf '%s\n' "  make status      Show Vagrant VM status"
	@printf '%s\n' "  make ping        Test Ansible connectivity to all nodes"
	@printf '%s\n' "  make prepare     Prepare Ubuntu nodes for Kubernetes"
	@printf '%s\n' "  make install     Install the 3-server K3s HA cluster"
	@printf '%s\n' "  make verify      Show nodes and pods from inside the cluster"
	@printf '%s\n' "  make kubeconfig  Fetch kubeconfig to $(KUBECONFIG)"
	@printf '%s\n' "  make k3s-bin     Fetch the K3s binary for host-side kubectl fallback"
	@printf '%s\n' "  make nodes       Show nodes through SSH on node1"
	@printf '%s\n' "  make pods        Show pods through SSH on node1"
	@printf '%s\n' "  make local-nodes Show nodes from the host using fetched kubeconfig"
	@printf '%s\n' "  make local-pods  Show pods from the host using fetched kubeconfig"
	@printf '%s\n' "  make down        Stop the VMs"
	@printf '%s\n' "  make destroy     Delete the VMs"
	@printf '%s\n' ""
	@printf '%s\n' "Set INVENTORY=inventory.example.ini or another inventory file for non-Vagrant deployments."

up:
	$(VAGRANT) up

status:
	$(VAGRANT) status

ssh-node1:
	$(VAGRANT) ssh node1

ping:
	cd $(ANSIBLE_DIR) && $(ANSIBLE) k3s_nodes -m ping

prepare:
	cd $(ANSIBLE_DIR) && $(ANSIBLE_PLAYBOOK) prepare.yml

install:
	cd $(ANSIBLE_DIR) && $(ANSIBLE_PLAYBOOK) install-k3s.yml

verify:
	cd $(ANSIBLE_DIR) && $(ANSIBLE_PLAYBOOK) verify-k3s.yml

kubeconfig:
	mkdir -p k3s .ansible/tmp
	cd $(ANSIBLE_DIR) && $(ANSIBLE_PLAYBOOK) fetch-kubeconfig.yml
	chmod 600 $(KUBECONFIG)

k3s-bin:
	mkdir -p k3s .ansible/tmp
	cd $(ANSIBLE_DIR) && $(ANSIBLE) node1 -b -m fetch -a "src=/usr/local/bin/k3s dest=../$(K3S_BIN) flat=true"
	chmod 755 $(K3S_BIN)

nodes:
	cd $(ANSIBLE_DIR) && $(ANSIBLE) node1 -b -m command -a "k3s kubectl get nodes -o wide"

pods:
	cd $(ANSIBLE_DIR) && $(ANSIBLE) node1 -b -m command -a "k3s kubectl get pods -A -o wide"

local-nodes:
	@if [ -z "$(KUBECTL)" ]; then \
		printf '%s\n' "kubectl is not installed on the host. Use 'make nodes' over SSH, or install kubectl."; \
		exit 127; \
	fi
	KUBECONFIG=$(KUBECONFIG) $(KUBECTL) get nodes -o wide

local-pods:
	@if [ -z "$(KUBECTL)" ]; then \
		printf '%s\n' "kubectl is not installed on the host. Use 'make pods' over SSH, or install kubectl."; \
		exit 127; \
	fi
	KUBECONFIG=$(KUBECONFIG) $(KUBECTL) get pods -A -o wide

down:
	$(VAGRANT) halt

destroy:
	$(VAGRANT) destroy

clean:
	rm -f $(KUBECONFIG) $(K3S_BIN)
