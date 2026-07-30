# custom-tenant-operator

## Prerequisites
### K3d cluster
K3d cluster check
```bash
k3d cluster list
kubectl cluster-info
```
To create a cluster
```bash
k3d cluster create tenant-dev --agents 2
kubectl get nodes   # should show 1 server + 2 agent nodes, all Ready
```

### Kubebuilder
Installation
```bash
# Download the latest release for linux/amd64 (WSL2 runs as Linux)
curl -L -o kubebuilder "https://github.com/kubernetes-sigs/kubebuilder/releases/latest/download/kubebuilder_linux_amd64"
chmod +x kubebuilder
sudo mv kubebuilder /usr/local/bin/

# Verify
kubebuilder version
```

### Go
Installation
```bash
curl -L -o go.tar.gz https://go.dev/dl/go1.22.5.linux-amd64.tar.gz
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc
go version
```
## Kubebuilder imit
```bash
kubebuilder init --domain platform.io --repo github.com/dakshina13/custom-tenant-operator
```
**Description:** Scaffolds a new Kubebuilder operator project and configures base settings.

- **Project:** Creates `PROJECT` and standard scaffold files (Makefile, `cmd/`, `config/`)
- **Domain:** Sets API domain to `platform.io` for CRD API group names
- **Module:** Sets Go module root to `github.com/dakshina13/custom-tenant-operator`
- **Boilerplate:** Initializes `go.mod` and writes controller-runtime main/manager wiring
- **Next steps:** Prepares the workspace for `kubebuilder create api` and `kubebuilder create webhook`