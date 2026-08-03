# Custom Tenant Operator

A Kubernetes Operator built with Kubebuilder that automates multi-tenant namespace provisioning — RBAC, resource quotas, and lifecycle management — via a single declarative `Tenant` custom resource.

## What it does

Given a `Tenant` object:

```yaml
apiVersion: platform.platform.io/v1alpha1
kind: Tenant
metadata:
  name: team-payments
spec:
  displayName: "Payments Team"
  users:
    - name: alice
      role: admin
    - name: bob
      role: view
  quota:
    cpu: "4"
    memory: "8Gi"
```

the operator reconciles:
- A **Namespace** matching the Tenant name
- A **ResourceQuota** enforcing `spec.quota` limits
- A **RoleBinding** per user, mapped to Kubernetes' built-in `admin`/`edit`/`view` ClusterRoles

Removing a user from `spec.users` prunes their RoleBinding on the next reconcile. Deleting the `Tenant` triggers finalizer-based cleanup before the object is removed.

## Design decisions worth calling out

**`Tenant` is cluster-scoped, not namespace-scoped.** Originally namespace-scoped, this broke owner-reference garbage collection: Kubernetes forbids a namespace-scoped object from owning a cluster-scoped one (like a `Namespace`), since deleting the owner's own namespace could otherwise cascade-delete an unrelated namespace. Since tenancy is inherently a cluster-wide administrative concern, `Tenant` was switched to cluster-scoped — matching the pattern used by comparable projects like Capsule — which makes owner-reference cascade deletion work correctly for the Namespace.

**Owner references vs. finalizers — used for different jobs.** RBAC objects and the ResourceQuota use `SetControllerReference`, giving free cascade-delete via Kubernetes' built-in garbage collector. The Namespace itself is cleaned up explicitly via a **finalizer** (`platform.platform.io/tenant-finalizer`) rather than relying solely on owner-reference GC — this gives the reconciler an observable, ordered hook for cleanup rather than depending entirely on implicit background garbage collection.

**Status conditions follow the standard `metav1.Condition` pattern**, using `meta.SetStatusCondition` to avoid a subtle but important bug: an early version unconditionally wrote status on every reconcile, which — because any write bumps `resourceVersion` and triggers a fresh watch event — created a self-sustaining reconcile loop. The fix was ensuring status writes only fire when the condition's value actually changes.

**RBAC prune-on-removal.** The reconciler diffs the desired `RoleBinding` set (from current `spec.users`) against what's actually labeled as belonging to the Tenant, and deletes anything no longer declared — since naive "create what's in spec" logic never removes bindings for users who've been taken off a Tenant.

## Testing

`internal/controller/tenant_controller_test.go` uses `envtest` (a real, ephemeral `kube-apiserver` + `etcd`, no Docker) with Ginkgo/Gomega, covering Namespace/Quota/RBAC provisioning, prune-on-removal, and finalizer-driven deletion.

One environment-specific note: `envtest` doesn't run the namespace lifecycle controller, so a deleted Namespace never fully leaves `Terminating` state within the test process. Tests use unique per-run Tenant names to avoid collisions rather than waiting on Namespace deletion to complete — this is a test-harness limitation, not a gap in the reconciler (verified separately against a real k3d cluster, where Namespace deletion completes normally).

```bash
make test
```

## Local development (WSL2 + k3d)

```bash
k3d cluster create tenant-dev --agents 2
make install   # apply the CRD
make run       # run the controller locally against k3d
```

```bash
kubectl apply -f config/samples/platform_v1alpha1_tenant.yaml
kubectl get tenants
kubectl get namespace,resourcequota,rolebinding -n team-payments
```

## Known limitations / possible extensions

- RBAC subjects are Kubernetes `User` kind — real enforcement depends on cluster auth (OIDC, client certs) mapping usernames accordingly
- No NetworkPolicy or service-mesh-level isolation between tenants yet
- Role *changes* for an existing user (not just removal) currently create a new RoleBinding and prune the old one, rather than patching in place — a side effect of deterministic naming that encodes the role

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