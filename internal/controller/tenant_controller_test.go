package controller

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	platformv1alpha1 "github.com/dakshina13/custom-tenant-operator/api/v1alpha1"
)

var _ = Describe("Tenant Controller", func() {
	var tenantName string

	BeforeEach(func() {
		tenantName = fmt.Sprintf("test-tenant-%d", time.Now().UnixNano())
	})

	ctx := context.Background()

	AfterEach(func() {
		// Clean up between tests so each Describe/It starts fresh.
		tenant := &platformv1alpha1.Tenant{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: tenantName}, tenant)
		if err == nil {
			Expect(k8sClient.Delete(ctx, tenant)).To(Succeed())
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: tenantName}, tenant)
				return apierrors.IsNotFound(err)
			}, "5s", "100ms").Should(BeTrue())
		}
		// Deliberately NOT waiting for the Namespace to disappear — envtest has
		// no namespace lifecycle controller, so a Terminating namespace never
		// actually completes deletion. Using unique names per test (via
		// BeforeEach) avoids collisions instead.
	})

	Context("When creating a Tenant", func() {
		It("should provision a Namespace with the same name", func() {
			tenant := &platformv1alpha1.Tenant{
				ObjectMeta: metav1.ObjectMeta{Name: tenantName},
				Spec: platformv1alpha1.TenantSpec{
					DisplayName: "Test Tenant",
					Users: []platformv1alpha1.TenantUser{
						{Name: "alice", Role: "admin"},
					},
					Quota: platformv1alpha1.ResourceQuotaSpec{
						CPU:    "2",
						Memory: "4Gi",
					},
				},
			}
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

			ns := &corev1.Namespace{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: tenantName}, ns)
			}, "5s", "100ms").Should(Succeed())
		})

		It("should provision a ResourceQuota matching spec.quota", func() {
			tenant := &platformv1alpha1.Tenant{
				ObjectMeta: metav1.ObjectMeta{Name: tenantName},
				Spec: platformv1alpha1.TenantSpec{
					DisplayName: "Test Tenant",
					Quota:       platformv1alpha1.ResourceQuotaSpec{CPU: "2", Memory: "4Gi"},
				},
			}
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

			quota := &corev1.ResourceQuota{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "tenant-quota", Namespace: tenantName}, quota)
			}, "5s", "100ms").Should(Succeed())

			requestsCPU := quota.Spec.Hard[corev1.ResourceRequestsCPU]
			limitsCPU := quota.Spec.Hard[corev1.ResourceLimitsCPU]
			requestsMem := quota.Spec.Hard[corev1.ResourceRequestsMemory]
			limitsMem := quota.Spec.Hard[corev1.ResourceLimitsMemory]

			Expect(requestsCPU.String()).To(Equal("2"))
			Expect(limitsCPU.String()).To(Equal("2"))
			Expect(requestsMem.String()).To(Equal("4Gi"))
			Expect(limitsMem.String()).To(Equal("4Gi"))
		})

		It("should create a RoleBinding for each user", func() {
			tenant := &platformv1alpha1.Tenant{
				ObjectMeta: metav1.ObjectMeta{Name: tenantName},
				Spec: platformv1alpha1.TenantSpec{
					DisplayName: "Test Tenant",
					Users: []platformv1alpha1.TenantUser{
						{Name: "alice", Role: "admin"},
						{Name: "bob", Role: "view"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

			rb := &rbacv1.RoleBinding{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "tenant-alice-admin", Namespace: tenantName}, rb)
			}, "5s", "100ms").Should(Succeed())
			Expect(rb.RoleRef.Name).To(Equal("admin"))
			Expect(rb.Subjects[0].Name).To(Equal("alice"))
		})
	})

	Context("When a user is removed from spec.users", func() {
		It("should prune the corresponding RoleBinding", func() {
			tenant := &platformv1alpha1.Tenant{
				ObjectMeta: metav1.ObjectMeta{Name: tenantName},
				Spec: platformv1alpha1.TenantSpec{
					DisplayName: "Test Tenant",
					Users: []platformv1alpha1.TenantUser{
						{Name: "alice", Role: "admin"},
						{Name: "bob", Role: "view"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

			rb := &rbacv1.RoleBinding{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "tenant-bob-view", Namespace: tenantName}, rb)
			}, "5s", "100ms").Should(Succeed())

			// Remove bob from spec.users and update
			Eventually(func() error {
				latest := &platformv1alpha1.Tenant{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: tenantName}, latest); err != nil {
					return err
				}
				latest.Spec.Users = []platformv1alpha1.TenantUser{
					{Name: "alice", Role: "admin"},
				}
				return k8sClient.Update(ctx, latest)
			}, "5s", "100ms").Should(Succeed())

			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "tenant-bob-view", Namespace: tenantName}, rb)
				return apierrors.IsNotFound(err)
			}, "5s", "100ms").Should(BeTrue())
		})
	})

	Context("When a Tenant is deleted", func() {
		It("should clean up the Namespace via the finalizer", func() {
			tenant := &platformv1alpha1.Tenant{
				ObjectMeta: metav1.ObjectMeta{Name: tenantName},
				Spec:       platformv1alpha1.TenantSpec{DisplayName: "Test Tenant"},
			}
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

			// Wait for finalizer to be added
			Eventually(func() []string {
				latest := &platformv1alpha1.Tenant{}
				_ = k8sClient.Get(ctx, types.NamespacedName{Name: tenantName}, latest)
				return latest.Finalizers
			}, "5s", "100ms").Should(ContainElement(tenantFinalizer))

			Expect(k8sClient.Delete(ctx, tenant)).To(Succeed())

			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: tenantName}, &platformv1alpha1.Tenant{})
			}, "5s", "100ms").Should(MatchError(ContainSubstring("not found")))
		})
	})
})
