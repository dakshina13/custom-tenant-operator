## Creation of New CRD
```bash
kubebuilder create api --group platform --version v1alpha1 --kind Tenant --resource --controller
```
## Applying tenant object
```bash
kubectl apply -f config/samples/platform_v1alpha1_tenant.yaml
kubectl get tenants
kubectl get tenant team-payments -o yaml
```

## Go run console output explanation
```
Explain the output of make run logs

(base) dm@DM:~/custom-tenant-operator$ make run
"/home/dm/custom-tenant-operator/bin/controller-gen" rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases
"/home/dm/custom-tenant-operator/bin/controller-gen" object:headerFile="hack/boilerplate.go.txt",year=2026 paths="./..."
go fmt ./...
go vet ./...
go run ./cmd/main.go
2026-07-30T11:56:42+01:00       INFO    setup   Starting manager
2026-07-30T11:56:42+01:00       INFO    starting server {"name": "health probe", "addr": "[::]:8081"}
2026-07-30T11:56:42+01:00       INFO    Starting EventSource    {"controller": "tenant", "controllerGroup": "platform.platform.io", "controllerKind": "Tenant", "source": "kind source: *v1alpha1.Tenant"}
2026-07-30T11:56:42+01:00       INFO    Starting Controller     {"controller": "tenant", "controllerGroup": "platform.platform.io", "controllerKind": "Tenant"}
2026-07-30T11:56:42+01:00       INFO    Starting workers        {"controller": "tenant", "controllerGroup": "platform.platform.io", "controllerKind": "Tenant", "worker count": 1}
2026-07-30T11:56:42+01:00       INFO    reconciling Tenant      {"controller": "tenant", "controllerGroup": "platform.platform.io", "controllerKind": "Tenant", "Tenant": {"name":"team-payments","namespace":"default"}, "namespace": "default", "name": "team-payments", "reconcileID": "75c62259-f84d-4341-907b-90b92d3a3968", "name": "team-payments", "displayName": "Payments Team"}
2026-07-30T11:57:42+01:00       INFO    Tenant not found, likely deleted        {"controller": "tenant", "controllerGroup": "platform.platform.io", "controllerKind": "Tenant", "Tenant": {"name":"team-payments","namespace":"default"}, "namespace": "default", "name": "team-payments", "reconcileID": "d326fe59-edf4-4699-8f69-b660bba45a03", "name": "team-payments"}
2026-07-30T11:57:50+01:00       INFO    reconciling Tenant      {"controller": "tenant", "controllerGroup": "platform.platform.io", "controllerKind": "Tenant", "Tenant": {"name":"team-payments","namespace":"default"}, "namespace": "default", "name": "team-payments", "reconcileID": "59bcf832-421c-4ae0-a330-ca51f14ae677", "name": "team-payments", "displayName": "Payments Team"}
^C2026-07-30T11:58:00+01:00     INFO    Stopping and waiting for non leader election runnables
2026-07-30T11:58:00+01:00       INFO    Stopping and waiting for warmup runnables
2026-07-30T11:58:00+01:00       INFO    Stopping and waiting for leader election runnables
2026-07-30T11:58:00+01:00       INFO    Shutdown signal received, waiting for all workers to finish     {"controller": "tenant", "controllerGroup": "platform.platform.io", "controllerKind": "Tenant"}
2026-07-30T11:58:00+01:00       INFO    All workers finished    {"controller": "tenant", "controllerGroup": "platform.platform.io", "controllerKind": "Tenant"}
2026-07-30T11:58:00+01:00       INFO    Stopping and waiting for caches
2026-07-30T11:58:00+01:00       INFO    Stopping and waiting for webhooks
2026-07-30T11:58:00+01:00       INFO    Stopping and waiting for HTTP servers
2026-07-30T11:58:00+01:00       INFO    shutting down server    {"name": "health probe", "addr": "[::]:8081"}
2026-07-30T11:58:00+01:00       INFO    Wait completed, proceeding to shutdown the manager
make: *** [Makefile:117: run] Error 1
```
This is a clean run end-to-end — let's walk through it line by line.

## Pre-flight (before your controller even starts)

```
controller-gen ... crd webhook paths="./..."
controller-gen ... object:headerFile=...
go fmt ./...
go vet ./...
go run ./cmd/main.go
```
`make run` isn't *just* "run the binary" — the Makefile target chains `manifests`, `generate`, `fmt`, `vet` as dependencies first, then finally runs `go run ./cmd/main.go`. So every `make run` re-syncs your CRD YAML and deepcopy code from your types before starting, and catches formatting/vet issues before even attempting to run. That's why you see all that output before "Starting manager."

## Manager startup

```
INFO  setup  Starting manager
INFO  starting server  {"name": "health probe", "addr": "[::]:8081"}
```
The manager process is up, and it's exposing a health/readiness probe endpoint on port 8081 — this is what Kubernetes would hit with liveness/readiness checks once this runs as an actual in-cluster Deployment (Phase 6+). Right now nothing's checking it since you're running locally, but the endpoint still starts.

```
INFO  Starting EventSource  {..., "source": "kind source: *v1alpha1.Tenant"}
INFO  Starting Controller   {...}
INFO  Starting workers      {..., "worker count": 1}
```
This is `SetupWithManager()`'s `.For(&Tenant{})` taking effect — the controller establishes a **watch** on the `Tenant` kind against the API server (an "EventSource"), then starts its worker pool (1 goroutine by default) that will pull items off the internal work queue and call your `Reconcile()`.

## First reconcile — your `kubectl apply`

```
INFO  reconciling Tenant  {..., "name": "team-payments", "displayName": "Payments Team"}
```
This fired the moment your `kubectl apply` landed. Note the timestamp — `11:56:42`, same second as manager startup. That's expected: on startup, controller-runtime's cache does an initial **List** of all existing `Tenant` objects and queues a reconcile for each one it finds, in addition to watching for future changes. So even if you'd applied the sample *before* starting `make run`, you'd still get an immediate reconcile on startup — this is actually important conceptually: **the controller doesn't rely on having "seen" the create event live; it reconciles based on current state whenever it starts**, which ties back to the level-triggered idea from earlier.

## Delete → reconcile fires again

```
INFO  Tenant not found, likely deleted  {..., "name": "team-payments"}
```
Your `kubectl delete` triggered another reconcile call (deletion is itself a watched event), and this time `r.Get()` returned `NotFound`, hitting the branch you wrote for that case. Note it's still a `Reconcile()` call even though the object is gone — this is exactly the "we don't get an event *type*, we get a name, and now the name resolves to nothing" behavior discussed earlier.

## Re-apply → reconciles again

```
INFO  reconciling Tenant  {..., "name": "team-payments", "displayName": "Payments Team"}
```
Your second `kubectl apply` fired a fresh reconcile, correctly picking the object back up.

**One interesting detail:** there's roughly a minute gap between the "not found" log and this one (`11:57:42` → `11:57:50` is actually only 8s, but note the delete happened at some point before that — worth noting your delete/re-apply commands themselves took some real wall-clock time between typing them, this isn't the controller being slow).

## Shutdown

```
^C2026-07-30T11:58:00 INFO Stopping and waiting for non leader election runnables
...
INFO  Wait completed, proceeding to shutdown the manager
make: *** [Makefile:117: run] Error 1
```

This is **not a real error** — it's just how `make` reports that the underlying `go run` process exited with a non-zero code because you sent it `SIGINT` (Ctrl+C). The manager did a graceful shutdown: stopped accepting new work, waited for in-flight reconciles to finish, closed watches, stopped the health probe server, then exited. `Make` interprets any non-zero exit as target failure and prints `Error 1`, but the shutdown sequence itself is exactly what a well-behaved graceful termination should look like — this is the same shutdown path Kubernetes would trigger via `SIGTERM` on pod termination once this runs in-cluster.

---

**Bottom line: this worked perfectly.** Fetch → log → not-found handling → re-fetch, all confirmed live against your k3d cluster, with the manager's watch/reconcile/shutdown lifecycle behaving exactly as expected.

## 1. What the `controller-gen` lines signify

`controller-gen` is a separate code/manifest generation tool (not part of the standard Go toolchain) that kubebuilder relies on. It's the actual engine behind `make manifests` and `make generate` — the Makefile just invokes it with different flags. You're seeing both invocations because `make run` depends on both targets before starting the manager.

**Line 1:**
```
controller-gen rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases
```
This is the `make manifests` step. It walks (`paths="./..."`) all your Go packages looking for `+kubebuilder:*` marker comments, and generates three categories of YAML from them:
- `rbac:roleName=manager-role` → regenerates `config/rbac/role.yaml`, built from `+kubebuilder:rbac:groups=...` markers (you haven't added any yet — this'll matter once your reconciler actually needs permission to create Namespaces/RoleBindings in Phase 2/3)
- `crd` → regenerates `config/crd/bases/platform.platform.io_tenants.yaml` from your struct tags and validation markers — this is what made your enum/required-field validation actually work
- `webhook` → scaffolds webhook manifest config *if* you have any `+kubebuilder:webhook` markers defined (you don't yet, so this is a no-op for now, but the tool always checks)

**Line 2:**
```
controller-gen object:headerFile="hack/boilerplate.go.txt",year=2026 paths="./..."
```
This is the `make generate` step. `object` here means "generate `DeepCopyObject()` implementations" — this is what produces/updates `zz_generated.deepcopy.go`. The `headerFile` flag just stamps the standard license/boilerplate comment at the top of the generated file with the given year.

So: same tool, two different generator modes, both re-run automatically as prerequisites every time you `make run`, ensuring your CRD YAML and deepcopy code never drift out of sync with your `tenant_types.go` even if you forgot to run them manually after an edit.

## 2. Are admission webhooks forwarding events to the manager?

No — and this is a good thing to disentangle, because the word "webhook" is doing double duty here in a confusing way.

**What actually delivers events to your controller** is the **watch mechanism**, not any kind of webhook:
1. Your manager's client establishes a **long-lived HTTP watch connection** to the API server (`GET /apis/platform.platform.io/v1alpha1/tenants?watch=true`)
2. The API server streams change events (Added/Modified/Deleted) down that connection as they happen in etcd
3. controller-runtime's informer/cache layer receives these, updates its local cache, and enqueues a reconcile request (`namespace/name`) onto the work queue
4. Your worker goroutine pulls from that queue and calls `Reconcile()`

This is **pull-based watching**, not the API server *pushing* an HTTP call *into* your controller. Your controller reached out and subscribed; nothing is calling back into you.

**What "webhook" means in the `controller-gen` output is a completely different mechanism** — **admission webhooks**. These are an *optional* feature where the API server, at the moment someone runs `kubectl apply`, synchronously calls out to an HTTP endpoint *you'd* have to implement (a `ValidatingWebhookConfiguration` or `MutatingWebhookConfiguration`) to ask "should this object be allowed?" or "here's a chance to modify it before storage." This is how, for example, you could implement custom validation logic beyond what OpenAPI schema enums can express — but it requires you to run an HTTPS server with a valid cert, register a `ValidatingWebhookConfiguration`, etc. You haven't scaffolded any of that (no `+kubebuilder:webhook` markers exist in your project yet), so `controller-gen`'s `webhook` generator target ran but had nothing to do.

**The distinction that matters:**
- **Watches** (what's actually happening in your project right now): API server → controller, pull-based, asynchronous, used for reconciliation
- **Admission webhooks** (not present yet, optional future addition): API server → your webhook server, push-based, *synchronous* (blocks the `kubectl apply` until it responds), used for validation/mutation *before* an object is even stored

If you ever want to add, say, cross-field validation that OpenAPI enums can't express (e.g., "if `role: admin`, `quota.cpu` must be at least 2") — that's exactly the use case where you'd add an actual admission webhook later. Not needed for this project's core scope, but worth knowing the two concepts are unrelated even though `controller-gen`'s flag happens to be named `webhook`.

# Kubebuilder markers explanation
These three marker comments are instructions to `controller-gen`, telling it what to put into `config/rbac/role.yaml` when you run `make manifests`. Let's break down the syntax and why each line exists.

## The syntax pattern

```go
// +kubebuilder:rbac:groups=<API_GROUP>,resources=<RESOURCE_TYPE>,verbs=<VERB_LIST>
```

Each marker generates one `PolicyRule` entry in a `ClusterRole` (or `Role`, depending on config) that gets bound to your controller's ServiceAccount. This is exactly the RBAC model you already know from HPE — `apiGroups`, `resources`, `verbs` — just expressed as Go comments that get compiled into YAML instead of written by hand.

## Line by line

**Line 1:**
```go
// +kubebuilder:rbac:groups=platform.platform.io,resources=tenants,verbs=get;list;watch;create;update;patch;delete
```
This grants the controller full CRUD access to `Tenant` objects themselves, in your custom API group `platform.platform.io`. Your controller needs `get`/`list`/`watch` at minimum to fetch and watch Tenants (which you're already doing via `r.Get()`), and you're including the write verbs (`create`/`update`/`patch`/`delete`) preemptively in case you later have the controller modify Tenant objects directly (you don't yet, but it's a common pattern to grant broadly here since the controller genuinely owns this resource type).

**Line 2:**
```go
// +kubebuilder:rbac:groups=platform.platform.io,resources=tenants/status,verbs=get;update;patch
```
This is a **separate permission** for the `/status` **subresource** specifically. Recall from earlier: because your `Tenant` CRD has `+kubebuilder:subresource:status`, the `spec` and `status` fields are actually two distinct API endpoints under the hood (`/tenants/{name}` vs `/tenants/{name}/status`), and Kubernetes RBAC treats them as **separately permissioned resources** — `tenants` and `tenants/status` are not the same grant. This is intentional design: it lets you have controllers/users that can *read/write spec* without being able to *write status* (or vice versa — e.g., you might let a CI pipeline create Tenants but only the controller itself update their status conditions). You'll actually need this line for real once Phase 5 has your reconciler calling `r.Status().Update()`.

**Line 3:**
```go
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;create;update;patch;delete
```
This is the one that actually matters *right now* for Increment 2 — permission to manage `Namespace` objects. Note `groups=core` (sometimes written as an empty string `""` in raw RBAC YAML) — `Namespace`, `Pod`, `Service`, `ConfigMap`, etc. all live in the unnamed/"core" API group, unlike your custom `Tenant` which lives in `platform.platform.io`. Without this line, `make manifests` wouldn't add Namespace permissions to `config/rbac/role.yaml`, and once you eventually run this controller **in-cluster** (via `make deploy`, using its own ServiceAccount rather than your kubeconfig), it would get a `403 Forbidden` the moment `Reconcile()` tried to create a Namespace.

**Line 3:**

```go
// +kubebuilder:rbac:groups=platform.platform.io,resources=tenants/finalizers,verbs=update
```

Same pattern as the `tenants/status` line — `finalizers` is **another subresource**, separate from both `spec` and `status`. When your reconciler adds or removes a finalizer string from `metadata.finalizers` (which you'll do in Phase 4, to intercept deletion and run cleanup logic before the object is actually removed), that write goes through the `tenants/finalizers` subresource endpoint specifically, not the main `tenants` resource. Kubernetes RBAC requires this permission granted separately — the same subresource-isolation reasoning as `status`, just applied to a different field.

## Why this doesn't affect `make run` right now

`make run` executes the controller as a plain local process using **your own kubeconfig identity** — which, as cluster admin on your k3d cluster, can do anything regardless of what these markers say. So Increment 2 will work fine locally even with wrong/missing RBAC markers. These markers only get *enforced* once the controller runs as its own ServiceAccount inside the cluster (Phase 6+, `make deploy`). Getting them right now is about habit and correctness of the generated `config/rbac/role.yaml`, not something that'll block today's test.

## Where the generated output lands

After `make manifests`, check `config/rbac/role.yaml` — you should see three corresponding `PolicyRule` blocks like:

```yaml
- apiGroups: ["platform.platform.io"]
  resources: ["tenants"]
  verbs: ["get","list","watch","create","update","patch","delete"]
- apiGroups: ["platform.platform.io"]
  resources: ["tenants/status"]
  verbs: ["get","update","patch"]
- apiGroups: [""]
  resources: ["namespaces"]
  verbs: ["get","list","watch","create","update","patch","delete"]
```

Worth a quick look after you run `make manifests` to confirm they landed — that's the file that'll actually get applied as the controller's `ClusterRole` when you eventually `make deploy`.