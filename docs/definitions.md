Good idea to nail the concepts down before writing code — this'll also make you much sharper in interviews. Let's break it into four pieces.

## 1. What is a Custom Resource Definition (CRD)?

Kubernetes ships with built-in resource types — `Pod`, `Deployment`, `Service`, `ConfigMap`, etc. A **CRD** lets you extend the Kubernetes API with your *own* resource type, so that `kubectl apply -f my-thing.yaml` works for something Kubernetes has never heard of natively.

When you define a CRD called `Tenant`, you're telling the Kubernetes API server: "add a new endpoint, `/apis/platform.io/v1alpha1/tenants`, and here's the schema for what a valid `Tenant` object looks like." After that, `Tenant` objects behave exactly like built-in ones — they're stored in etcd, you can `kubectl get tenants`, apply RBAC to them, watch them for changes, etc.

**Key point:** a CRD by itself does *nothing*. It's pure data — a schema and a place to store objects. If you `kubectl apply` a `Tenant` with no controller running, it just sits in etcd inertly. Nothing reacts to it.

## 2. What is a Custom Operator (Controller)?

An **operator** (or **custom controller**) is the piece of software that gives a CRD *behavior*. It's a program running in your cluster that:

1. **Watches** for `Tenant` objects being created, updated, or deleted (via the Kubernetes API's watch mechanism)
2. **Reconciles** — compares "what the user asked for" (`spec`) against "what actually exists" in the cluster, and takes action to close the gap
3. Loops forever, because it's **level-triggered, not event-triggered** — it doesn't just react once to a create event, it continuously re-checks that reality matches the desired spec, self-healing drift

The term "operator" specifically implies a controller that encodes *operational knowledge* — the kind of thing a human ops engineer would do by hand (provision a namespace, set quotas, wire up RBAC) — and automates it. That's the "operator pattern": codifying a human operator's runbook into a control loop.

So: **CRD = the noun (the shape of the data). Operator = the verb (what happens when that data changes).**

## 3. The use case for *this* operator

Right now, onboarding a new team/tenant onto a shared cluster is a manual, multi-step, error-prone process: someone creates a namespace, remembers to set a ResourceQuota, manually writes RoleBindings for each team member, maybe forgets a NetworkPolicy. Every tenant can drift slightly from every other tenant depending on who set it up.

With the `Tenant` CRD + operator:
- A platform engineer (or a self-service portal) submits *one* declarative object:
```yaml
apiVersion: platform.io/v1alpha1
kind: Tenant
metadata:
  name: team-payments
spec:
  displayName: "Payments Team"
  users:
    - name: alice
      role: admin
    - name: bob
      role: viewer
  quota:
    cpu: "4"
    memory: "8Gi"
```
- The operator reconciles that into: a `Namespace`, a `ResourceQuota`, and `RoleBinding`s for each user — consistently, every time, with no manual steps
- Deleting the `Tenant` object cleanly tears everything down (via the finalizer we'll build in Phase 4)
- Drift correction is free: if someone manually deletes a RoleBinding, the controller notices on its next reconcile and recreates it

This is the exact pattern real platform teams use for **multi-tenancy on shared clusters** — it's sometimes called "Namespace-as-a-Service." It directly mirrors what you did with IAM/RBAC at HPE, just expressed as a Kubernetes-native control loop instead of a Go microservice calling out to an IAM system.

## 4. What does kubebuilder do?

Writing a controller from scratch means hand-rolling a lot of boilerplate: setting up a `client-go` informer/watch, building an API server that validates your CRD schema, writing the manager/leader-election scaffolding, generating CRD YAML from Go types, wiring RBAC for the controller's own service account, etc.

**Kubebuilder** is a scaffolding framework (built on top of `controller-runtime`, the same library Kubernetes' own controllers use internally) that generates all of that boilerplate for you, so you focus only on the reconciliation logic that's actually specific to your operator.

Concretely, when you run:
```bash
kubebuilder init --domain platform.io --repo github.com/dakshina13/tenant-operator
kubebuilder create api --group platform --version v1alpha1 --kind Tenant
```

It generates:
- `api/v1alpha1/tenant_types.go` — Go structs for your `Spec`/`Status`, which it uses to auto-generate the CRD YAML (`make manifests`)
- `controllers/tenant_controller.go` — a `Reconcile(ctx, req)` function stub, which is the *only* function you actually need to write logic in
- `main.go` — sets up the "manager" (the process that runs your controller, handles leader election if you scale to multiple replicas, wires up metrics/health endpoints)
- `config/` — Kustomize manifests for CRDs, RBAC for the controller's service account, and sample CRs
- Makefile targets like `make install` (installs CRDs into the cluster), `make run` (runs your controller locally against whatever cluster your kubeconfig points to — this is how you'll iterate against k3d), and `make docker-build`/`make deploy` for when you eventually run it *inside* the cluster as a Deployment

**In short:** kubebuilder is to operators what `kubebuilder create api` is to `rails generate scaffold` — it gives you a working skeleton with the plumbing already done, following Kubernetes' own conventions, so the code you write is 90% business logic and 10% ceremony.
