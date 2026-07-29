Here's a phased plan built specifically around WSL2 + k3d, structured so each phase ends with something demoable and a concrete learning outcome.

## Phase 0: Environment Setup (few hours)

**Tools to install in WSL2:**
- Docker (or use WSL2's built-in Docker Engine, not Docker Desktop, to keep it lightweight)
- `k3d` — lets you spin up multiple lightweight k3s clusters fast, great for iterating on operator teardown/rebuild
- `kubebuilder` (or `operator-sdk`, but kubebuilder is more idiomatic Go and better documented)
- `kubectl`, `kustomize`, `go` (1.22+)

**Setup:**
```bash
k3d cluster create tenant-dev --agents 2
kubectl cluster-info
```

**Learning outcome:** Understand k3d vs kind vs minikube tradeoffs (k3d is faster to boot, uses k3s under the hood, good for CI-like disposable clusters) — this itself is a good interview talking point since you're coming from real EKS experience.

---

## Phase 1: Scaffold the CRD (1 session)

```bash
kubebuilder init --domain platform.io --repo github.com/dakshina13/tenant-operator
kubebuilder create api --group platform --version v1alpha1 --kind Tenant --resource --controller
```

Define the `Tenant` spec:
```go
type TenantSpec struct {
    DisplayName string            `json:"displayName"`
    Users       []TenantUser      `json:"users"`
    Quota       ResourceQuotaSpec `json:"quota"`
}

type TenantUser struct {
    Name string `json:"name"`
    Role string `json:"role"` // e.g. "admin", "viewer", "editor"
}
```

**Learning outcome:** CRD structure, OpenAPI validation via kubebuilder markers (`+kubebuilder:validation:Required`, enums for `Role`), and how `make manifests` generates the CRD YAML from Go structs. Deploy the CRD to k3d and confirm `kubectl explain tenant.spec` works — that's your first real milestone.

---

## Phase 2: Basic Reconciler — Namespace + Quota (1–2 sessions)

Implement the reconcile loop to:
1. Create a `Namespace` named after the Tenant
2. Set an **owner reference** from Namespace back to the Tenant (so you understand ownership vs finalizers — different mechanisms, commonly confused)
3. Create a `ResourceQuota` in that namespace from `spec.quota`

Test cycle on k3d:
```bash
make install run   # runs controller locally against k3d, outside cluster — fastest iteration loop
kubectl apply -f config/samples/tenant.yaml
kubectl get ns
```

**Learning outcome:** The core reconciliation pattern — reconcile is level-triggered, not edge-triggered. Deliberately break something (delete the namespace manually) and watch the controller recreate it. This is the concept interviewers most often probe and most candidates get wrong when explaining.

---

## Phase 3: RBAC Provisioning (1 session)

For each entry in `spec.users`, create a `RoleBinding` referencing built-in ClusterRoles (`admin`, `edit`, `view`) scoped to the tenant namespace.

**Learning outcome:** This is where your HPE IAM/RBAC experience becomes directly reusable — you already know the RBAC model, so focus your learning time on how controller-runtime's client manages create-or-update idempotently (`controllerutil.CreateOrUpdate`), since naive reconcilers often duplicate resources on every loop.

---

## Phase 4: Finalizers — Safe Teardown (1 session, important)

Add a finalizer (`tenant.platform.io/finalizer`) so that deleting a `Tenant` triggers cleanup logic before Kubernetes garbage-collects it — even though owner references would handle namespace deletion automatically, implement explicit finalizer logic anyway so you can demonstrate the pattern (e.g., emit an event, deregister from an external system, or clean up cross-namespace resources that owner refs can't reach).

**Test on k3d:**
```bash
kubectl delete tenant my-tenant
kubectl get tenant my-tenant -o yaml   # should show deletionTimestamp + finalizer, not disappear instantly
```

**Learning outcome:** Finalizers vs owner references — when each applies. This distinction is a favorite "do you actually understand operators or did you copy a tutorial" interview question.

---

## Phase 5: Status Conditions + Observability (1 session)

Add `status.conditions` (`Ready`, `Provisioning`, `Failed`) following the standard `metav1.Condition` pattern, so `kubectl get tenants` shows real state via a printer column:

```go
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
```

**Learning outcome:** How status subresources work (`/status` is a separate API endpoint from `/spec` — this is why you need `.Status().Update()` not `.Update()`), and why this separation exists (prevents clients from racing controllers on the same object).

---

## Phase 6: Testing (1–2 sessions — don't skip this)

Write `envtest` + `ginkgo`/`gomega` tests that spin up a real (but ephemeral) API server without needing a full cluster:
- Reconcile creates namespace + quota + rolebindings correctly
- Deleting a Tenant triggers finalizer cleanup
- Updating `spec.users` adds/removes RoleBindings correctly (this tests your reconciler's diffing logic, not just creation)

**Learning outcome:** This phase alone is what makes the project credible on a CV — most personal-project operators have zero tests. Being able to say "envtest-based test suite" in an interview signals real engineering discipline.

---

## Phase 7 (stretch): NetworkPolicy or Istio isolation

Since you have Istio/Envoy experience, add either:
- A default-deny `NetworkPolicy` per tenant namespace, or
- An Istio `AuthorizationPolicy` scoping traffic to same-tenant only

Only do this if Phases 1–6 are solid — it's the differentiator, not the foundation.

---

## Suggested repo structure for the CV/portfolio

```
tenant-operator/
├── api/v1alpha1/tenant_types.go
├── controllers/tenant_controller.go
├── controllers/tenant_controller_test.go   ← show this off explicitly
├── config/crd, config/rbac, config/samples
├── docs/README.md   ← architecture diagram + design decisions
└── docs/demo.gif     ← 30-second terminal recording of create/delete on k3d
```

A short README explaining *why* you made each design decision (finalizer vs owner ref, status conditions, RBAC choices) matters more to interviewers than the code itself — it shows judgment, not just syntax.

---

**Total time estimate:** 8–12 focused sessions (roughly 3–4 weekends), Phases 1–6 are the core; Phase 7 is optional polish.

Want me to generate the actual kubebuilder scaffold code for Phase 1–2 so you have a working starting point in WSL?