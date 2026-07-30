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