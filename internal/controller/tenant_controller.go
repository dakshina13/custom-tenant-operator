/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	platformv1alpha1 "github.com/dakshina13/custom-tenant-operator/api/v1alpha1"
)

const tenantFinalizer = "platform.platform.io/tenant-finalizer"

func ptrProtocol(p corev1.Protocol) *corev1.Protocol { return &p }
func ptrIntOrString(port int32) *intstr.IntOrString {
	v := intstr.FromInt32(port)
	return &v
}

// TenantReconciler reconciles a Tenant object
type TenantReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *TenantReconciler) setCondition(ctx context.Context, tenant *platformv1alpha1.Tenant, condType string, status metav1.ConditionStatus, reason, message string) error {
	changed := meta.SetStatusCondition(&tenant.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: tenant.Generation,
	})
	if !changed {
		// Nothing actually changed — skip the write entirely.
		// This is what breaks the self-triggering loop: no write, no new
		// resourceVersion, no new watch event, no new reconcile.
		return nil
	}
	return r.Status().Update(ctx, tenant)
}

// +kubebuilder:rbac:groups=platform.platform.io,resources=tenants,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=platform.platform.io,resources=tenants/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=platform.platform.io,resources=tenants/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=resourcequotas,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=bind,resourceNames=admin;edit;view

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Tenant object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.24.1/pkg/reconcile
func (r *TenantReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	var tenant platformv1alpha1.Tenant
	if err := r.Get(ctx, req.NamespacedName, &tenant); err != nil {
		if apierrors.IsNotFound(err) {
			// Tenant was deleted — nothing to do (no finalizer logic yet, that's Phase 4)
			logger.Info("Tenant not found, likely deleted", "name", req.Name)
			return ctrl.Result{}, nil
		}
		// Some other error (e.g. API server hiccup) — requeue
		logger.Error(err, "unable to fetch Tenant")
		return ctrl.Result{}, err
	}

	// --- Handle deletion ---
	if !tenant.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&tenant, tenantFinalizer) {
			logger.Info("Tenant is being deleted, running cleanup", "name", tenant.Name)

			// Explicit, observable cleanup. Owner-reference GC would handle
			// the Namespace (and everything inside it) automatically anyway,
			// but doing it explicitly here means we control ordering, can log
			// each step, and have a hook for any future cleanup that owner
			// references can't reach (e.g. something outside the cluster).
			ns := &corev1.Namespace{}
			err := r.Get(ctx, client.ObjectKey{Name: tenant.Name}, ns)
			if err == nil {
				logger.Info("deleting Namespace as part of Tenant cleanup", "namespace", tenant.Name)
				if delErr := r.Delete(ctx, ns); delErr != nil && !apierrors.IsNotFound(delErr) {
					logger.Error(delErr, "failed to delete Namespace during cleanup", "namespace", tenant.Name)
					return ctrl.Result{}, delErr
				}
			} else if !apierrors.IsNotFound(err) {
				logger.Error(err, "unable to fetch Namespace during cleanup", "namespace", tenant.Name)
				return ctrl.Result{}, err
			}

			logger.Info("cleanup complete, removing finalizer", "name", tenant.Name)
			controllerutil.RemoveFinalizer(&tenant, tenantFinalizer)
			if err := r.Update(ctx, &tenant); err != nil {
				logger.Error(err, "unable to remove finalizer")
				return ctrl.Result{}, err
			}
		}
		// Finalizer removed (or was never present) — nothing more to do.
		// Kubernetes will now actually delete the Tenant object.
		return ctrl.Result{}, nil
	}

	// --- Ensure finalizer is present before doing any provisioning ---
	if !controllerutil.ContainsFinalizer(&tenant, tenantFinalizer) {
		logger.Info("adding finalizer", "name", tenant.Name)
		controllerutil.AddFinalizer(&tenant, tenantFinalizer)
		if err := r.Update(ctx, &tenant); err != nil {
			logger.Error(err, "unable to add finalizer")
			return ctrl.Result{}, err
		}
		// Adding the finalizer triggers another reconcile (the Update above
		// causes a watch event), so we can safely return here and let that
		// next pass continue with provisioning.
		return ctrl.Result{}, nil
	}

	logger.Info("reconciling Tenant", "name", tenant.Name, "displayName", tenant.Spec.DisplayName)

	// Removed the unconditional "Provisioning" write here — it was flipping
	// Ready False→True on literally every single reconcile, which is itself
	// a real change each time, guaranteeing a write and a fresh watch event
	// forever. The final "Ready=True" call at the bottom already handles the
	// steady-state case correctly (no-op once already True).
	if !meta.IsStatusConditionTrue(tenant.Status.Conditions, "Ready") {
		if err := r.setCondition(ctx, &tenant, "Ready", metav1.ConditionFalse, "Provisioning", "Reconciling tenant resources"); err != nil {
			logger.Error(err, "unable to set Provisioning condition")
			return ctrl.Result{}, err
		}
	}

	// --- Namespace ---
	// Define the namespace we want to exist for this tenant
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: tenant.Name,
		},
	}

	// CreateOrUpdate: fetches the object if it exists, applies the mutation, and
	// creates or patches as needed — this is what makes reconcile idempotent
	// instead of blindly calling Create() every time (which would error on the 2nd run).
	result, err := controllerutil.CreateOrUpdate(ctx, r.Client, ns, func() error {
		// SetControllerReference makes the Tenant the owner of this Namespace.
		// This gives us garbage-collection for free: deleting the Tenant will
		// cascade-delete the Namespace via Kubernetes' built-in owner-reference GC,
		// even before we add explicit finalizer logic in Phase 4.
		return controllerutil.SetControllerReference(&tenant, ns, r.Scheme)
	})
	if err != nil {
		logger.Error(err, "unable to reconcile Namespace", "namespace", tenant.Name)
		_ = r.setCondition(ctx, &tenant, "Ready", metav1.ConditionFalse, "Failed", "Failed to reconcile Namespace: "+err.Error())
		return ctrl.Result{}, err
	}

	logger.Info("reconciled Namespace", "namespace", ns.Name, "operation", result)

	// --- ResourceQuota ---
	// Only create a ResourceQuota if the tenant actually specified quota values.
	// An empty spec.quota (both fields blank) means "no quota enforced" — skip it
	// rather than creating a ResourceQuota with empty limits (which would behave
	// oddly / be misleading in kubectl describe).
	if tenant.Spec.Quota.CPU != "" || tenant.Spec.Quota.Memory != "" {
		quota := &corev1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "tenant-quota",
				Namespace: tenant.Name,
			},
		}

		quotaResult, err := controllerutil.CreateOrUpdate(ctx, r.Client, quota, func() error {
			hard := corev1.ResourceList{}
			if tenant.Spec.Quota.CPU != "" {
				cpuQty, parseErr := resource.ParseQuantity(tenant.Spec.Quota.CPU)
				if parseErr != nil {
					return parseErr
				}
				hard[corev1.ResourceRequestsCPU] = cpuQty
				hard[corev1.ResourceLimitsCPU] = cpuQty
			}
			if tenant.Spec.Quota.Memory != "" {
				memQty, parseErr := resource.ParseQuantity(tenant.Spec.Quota.Memory)
				if parseErr != nil {
					return parseErr
				}
				hard[corev1.ResourceRequestsMemory] = memQty
				hard[corev1.ResourceLimitsMemory] = memQty
			}
			quota.Spec.Hard = hard

			// ResourceQuota is namespace-scoped, Tenant is cluster-scoped —
			// this combination IS allowed (cluster-scoped owners can own
			// namespace-scoped dependents in any namespace).
			return controllerutil.SetControllerReference(&tenant, quota, r.Scheme)
		})
		if err != nil {
			logger.Error(err, "unable to reconcile ResourceQuota", "namespace", tenant.Name)
			_ = r.setCondition(ctx, &tenant, "Ready", metav1.ConditionFalse, "Failed", "Failed to reconcile ResourceQuota: "+err.Error())
			return ctrl.Result{}, err
		}
		logger.Info("reconciled ResourceQuota", "namespace", tenant.Name, "operation", quotaResult)
	}

	// --- NetworkPolicy: isolate tenant namespace traffic ---
	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tenant-isolation",
			Namespace: tenant.Name,
		},
	}

	npResult, err := controllerutil.CreateOrUpdate(ctx, r.Client, np, func() error {
		np.Spec = networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{}, // applies to all pods in this namespace
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					// Allow traffic only from pods within the same namespace
					From: []networkingv1.NetworkPolicyPeer{
						{PodSelector: &metav1.LabelSelector{}},
					},
				},
			},
			Egress: []networkingv1.NetworkPolicyEgressRule{
				{
					// Allow egress within the same namespace
					To: []networkingv1.NetworkPolicyPeer{
						{PodSelector: &metav1.LabelSelector{}},
					},
				},
				{
					// Allow DNS resolution (UDP/TCP 53) — without this, pods
					// can't resolve any hostnames at all, breaking almost
					// everything.
					To: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{},
							PodSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"k8s-app": "kube-dns"},
							},
						},
					},
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: ptrProtocol(corev1.ProtocolUDP), Port: ptrIntOrString(53)},
						{Protocol: ptrProtocol(corev1.ProtocolTCP), Port: ptrIntOrString(53)},
					},
				},
			},
		}
		return controllerutil.SetControllerReference(&tenant, np, r.Scheme)
	})
	if err != nil {
		logger.Error(err, "unable to reconcile NetworkPolicy", "namespace", tenant.Name)
		_ = r.setCondition(ctx, &tenant, "Ready", metav1.ConditionFalse, "Failed", "Failed to reconcile NetworkPolicy: "+err.Error())
		return ctrl.Result{}, err
	}
	logger.Info("reconciled NetworkPolicy", "namespace", tenant.Name, "operation", npResult)

	// --- RBAC: one RoleBinding per user ---
	desired := make(map[string]bool) // tracks which RoleBinding names *should* exist
	for _, user := range tenant.Spec.Users {
		rbName := "tenant-" + user.Name + "-" + user.Role
		desired[rbName] = true
		rb := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				// Deterministic name so re-reconciling finds the same object
				// instead of creating duplicates on every loop.
				Name:      "tenant-" + user.Name + "-" + user.Role,
				Namespace: tenant.Name,
			},
		}

		rbResult, err := controllerutil.CreateOrUpdate(ctx, r.Client, rb, func() error {
			rb.Labels = map[string]string{
				"platform.platform.io/tenant": tenant.Name,
			}
			rb.RoleRef = rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "ClusterRole",
				// This works because your enum values (admin, edit, view)
				// are literally the names of Kubernetes' built-in ClusterRoles —
				// no translation layer needed.
				Name: user.Role,
			}
			rb.Subjects = []rbacv1.Subject{
				{
					Kind:     rbacv1.UserKind,
					APIGroup: rbacv1.GroupName,
					Name:     user.Name,
				},
			}
			return controllerutil.SetControllerReference(&tenant, rb, r.Scheme)
		})
		if err != nil {
			logger.Error(err, "unable to reconcile RoleBinding", "user", user.Name, "role", user.Role)
			_ = r.setCondition(ctx, &tenant, "Ready", metav1.ConditionFalse, "Failed", "Failed to reconcile RoleBinding: "+err.Error())
			return ctrl.Result{}, err
		}
		logger.Info("reconciled RoleBinding", "namespace", tenant.Name, "user", user.Name, "role", user.Role, "operation", rbResult)
	}

	// --- Prune: delete any RoleBindings owned by this Tenant that are no
	// longer represented in spec.users ---
	var existingRBs rbacv1.RoleBindingList
	if err := r.List(ctx, &existingRBs,
		client.InNamespace(tenant.Name),
		client.MatchingLabels{"platform.platform.io/tenant": tenant.Name},
	); err != nil {
		logger.Error(err, "unable to list RoleBindings for pruning", "namespace", tenant.Name)
		return ctrl.Result{}, err
	}

	for _, existing := range existingRBs.Items {
		if !desired[existing.Name] {
			if err := r.Delete(ctx, &existing); err != nil && !apierrors.IsNotFound(err) {
				logger.Error(err, "unable to prune stale RoleBinding", "name", existing.Name)
				return ctrl.Result{}, err
			}
			logger.Info("pruned stale RoleBinding", "namespace", tenant.Name, "name", existing.Name)
		}
	}

	if err := r.setCondition(ctx, &tenant, "Ready", metav1.ConditionTrue, "Provisioned", "All tenant resources reconciled successfully"); err != nil {
		logger.Error(err, "unable to set Ready condition")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TenantReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.Tenant{}).
		Named("tenant").
		Complete(r)
}
