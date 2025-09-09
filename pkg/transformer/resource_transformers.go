package transformer

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jlpadilla/search-collector-ai/pkg/informer"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

// ResourceSpecificTransformer provides resource-specific transformation logic
type ResourceSpecificTransformer struct {
	baseTransformer *baseTransformer
	config          *TransformConfig
}

// NewResourceSpecificTransformer creates a new resource-specific transformer
func NewResourceSpecificTransformer(config *TransformConfig) Transformer {
	return &ResourceSpecificTransformer{
		baseTransformer: NewBaseTransformer(config).(*baseTransformer),
		config:          config,
	}
}

func (t *ResourceSpecificTransformer) Transform(event *informer.ResourceEvent) (*TransformedResource, error) {
	// Start with base transformation
	transformed, err := t.baseTransformer.Transform(event)
	if err != nil {
		return nil, err
	}
	
	// Apply resource-specific field extraction based on resource type
	resourceSpecificFields, err := t.extractResourceSpecificFields(event.Object, event.ResourceType)
	if err != nil {
		klog.V(4).Infof("Failed to extract resource-specific fields for %s: %v", event.ResourceKey, err)
		// Don't fail the transformation, just log the error
	} else {
		// Merge resource-specific fields with base fields
		if transformed.Fields == nil {
			transformed.Fields = make(map[string]interface{})
		}
		for k, v := range resourceSpecificFields {
			transformed.Fields[k] = v
		}
	}
	
	return transformed, nil
}

func (t *ResourceSpecificTransformer) DiscoverRelationships(resource *TransformedResource, obj runtime.Object) ([]ResourceRelationship, error) {
	return t.baseTransformer.DiscoverRelationships(resource, obj)
}

func (t *ResourceSpecificTransformer) GetSupportedTypes() []string {
	return []string{"*"}
}

// extractResourceSpecificFields extracts fields specific to each resource type (kubectl get equivalent)
func (t *ResourceSpecificTransformer) extractResourceSpecificFields(obj runtime.Object, resourceType string) (map[string]interface{}, error) {
	fields := make(map[string]interface{})
	
	switch resourceType {
	case "pods":
		return t.extractPodFields(obj)
	case "services":
		return t.extractServiceFields(obj)
	case "deployments":
		return t.extractDeploymentFields(obj)
	case "replicasets":
		return t.extractReplicaSetFields(obj)
	case "daemonsets":
		return t.extractDaemonSetFields(obj)
	case "statefulsets":
		return t.extractStatefulSetFields(obj)
	case "nodes":
		return t.extractNodeFields(obj)
	case "namespaces":
		return t.extractNamespaceFields(obj)
	case "configmaps":
		return t.extractConfigMapFields(obj)
	case "secrets":
		return t.extractSecretFields(obj)
	case "ingresses":
		return t.extractIngressFields(obj)
	case "persistentvolumes":
		return t.extractPVFields(obj)
	case "persistentvolumeclaims":
		return t.extractPVCFields(obj)
	default:
		// For unknown resource types, extract common fields
		return t.extractCommonFields(obj)
	}
	
	return fields, nil
}

// extractPodFields extracts fields equivalent to "kubectl get pods"
// Columns: NAME, READY, STATUS, RESTARTS, AGE
func (t *ResourceSpecificTransformer) extractPodFields(obj runtime.Object) (map[string]interface{}, error) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return nil, fmt.Errorf("object is not a Pod")
	}
	
	fields := make(map[string]interface{})
	
	// Ready count (ready/total)
	readyContainers := 0
	totalContainers := len(pod.Spec.Containers)
	for _, status := range pod.Status.ContainerStatuses {
		if status.Ready {
			readyContainers++
		}
	}
	fields["ready"] = fmt.Sprintf("%d/%d", readyContainers, totalContainers)
	fields["ready_count"] = readyContainers
	fields["total_containers"] = totalContainers
	
	// Status
	fields["status"] = string(pod.Status.Phase)
	
	// Restart count (sum of all container restarts)
	restarts := int32(0)
	for _, status := range pod.Status.ContainerStatuses {
		restarts += status.RestartCount
	}
	fields["restarts"] = restarts
	
	// Node
	fields["node"] = pod.Spec.NodeName
	
	// Pod IP
	fields["pod_ip"] = pod.Status.PodIP
	
	// Host IP
	fields["host_ip"] = pod.Status.HostIP
	
	// QoS Class
	fields["qos_class"] = string(pod.Status.QOSClass)
	
	// Container images
	var images []string
	for _, container := range pod.Spec.Containers {
		images = append(images, container.Image)
	}
	fields["images"] = images
	
	return fields, nil
}

// extractServiceFields extracts fields equivalent to "kubectl get services"
// Columns: NAME, TYPE, CLUSTER-IP, EXTERNAL-IP, PORT(S), AGE
func (t *ResourceSpecificTransformer) extractServiceFields(obj runtime.Object) (map[string]interface{}, error) {
	svc, ok := obj.(*corev1.Service)
	if !ok {
		return nil, fmt.Errorf("object is not a Service")
	}
	
	fields := make(map[string]interface{})
	
	// Service type
	fields["type"] = string(svc.Spec.Type)
	
	// Cluster IP
	fields["cluster_ip"] = svc.Spec.ClusterIP
	
	// External IP
	var externalIPs []string
	externalIPs = append(externalIPs, svc.Spec.ExternalIPs...)
	for _, ingress := range svc.Status.LoadBalancer.Ingress {
		if ingress.IP != "" {
			externalIPs = append(externalIPs, ingress.IP)
		}
		if ingress.Hostname != "" {
			externalIPs = append(externalIPs, ingress.Hostname)
		}
	}
	fields["external_ips"] = externalIPs
	
	// Ports
	var ports []string
	for _, port := range svc.Spec.Ports {
		portStr := strconv.Itoa(int(port.Port))
		if port.TargetPort.String() != "" && port.TargetPort.String() != portStr {
			portStr += ":" + port.TargetPort.String()
		}
		if port.Protocol != corev1.ProtocolTCP {
			portStr += "/" + string(port.Protocol)
		}
		ports = append(ports, portStr)
	}
	fields["ports"] = ports
	
	// Selector
	fields["selector"] = svc.Spec.Selector
	
	return fields, nil
}

// extractDeploymentFields extracts fields equivalent to "kubectl get deployments"
// Columns: NAME, READY, UP-TO-DATE, AVAILABLE, AGE
func (t *ResourceSpecificTransformer) extractDeploymentFields(obj runtime.Object) (map[string]interface{}, error) {
	deployment, ok := obj.(*appsv1.Deployment)
	if !ok {
		return nil, fmt.Errorf("object is not a Deployment")
	}
	
	fields := make(map[string]interface{})
	
	// Ready replicas
	fields["ready_replicas"] = deployment.Status.ReadyReplicas
	fields["replicas"] = deployment.Status.Replicas
	fields["desired_replicas"] = *deployment.Spec.Replicas
	
	// Ready status (ready/desired)
	fields["ready"] = fmt.Sprintf("%d/%d", deployment.Status.ReadyReplicas, *deployment.Spec.Replicas)
	
	// Up-to-date replicas
	fields["updated_replicas"] = deployment.Status.UpdatedReplicas
	
	// Available replicas
	fields["available_replicas"] = deployment.Status.AvailableReplicas
	
	// Strategy
	fields["strategy_type"] = string(deployment.Spec.Strategy.Type)
	
	// Selector
	if deployment.Spec.Selector != nil {
		fields["selector"] = deployment.Spec.Selector.MatchLabels
	}
	
	return fields, nil
}

// extractNodeFields extracts fields equivalent to "kubectl get nodes"
// Columns: NAME, STATUS, ROLES, AGE, VERSION
func (t *ResourceSpecificTransformer) extractNodeFields(obj runtime.Object) (map[string]interface{}, error) {
	node, ok := obj.(*corev1.Node)
	if !ok {
		return nil, fmt.Errorf("object is not a Node")
	}
	
	fields := make(map[string]interface{})
	
	// Status
	var status string
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			if condition.Status == corev1.ConditionTrue {
				status = "Ready"
			} else {
				status = "NotReady"
			}
			break
		}
	}
	fields["status"] = status
	
	// Roles
	var roles []string
	for label := range node.Labels {
		if strings.HasPrefix(label, "node-role.kubernetes.io/") {
			role := strings.TrimPrefix(label, "node-role.kubernetes.io/")
			if role == "" {
				role = strings.TrimPrefix(label, "node-role.kubernetes.io/")
			}
			roles = append(roles, role)
		}
	}
	if len(roles) == 0 {
		roles = []string{"<none>"}
	}
	fields["roles"] = roles
	
	// Kubernetes version
	fields["kubelet_version"] = node.Status.NodeInfo.KubeletVersion
	
	// OS info
	fields["os_image"] = node.Status.NodeInfo.OSImage
	fields["kernel_version"] = node.Status.NodeInfo.KernelVersion
	fields["container_runtime"] = node.Status.NodeInfo.ContainerRuntimeVersion
	
	// Capacity
	if node.Status.Capacity != nil {
		fields["cpu_capacity"] = node.Status.Capacity.Cpu().String()
		fields["memory_capacity"] = node.Status.Capacity.Memory().String()
		fields["pods_capacity"] = node.Status.Capacity.Pods().String()
	}
	
	// Allocatable
	if node.Status.Allocatable != nil {
		fields["cpu_allocatable"] = node.Status.Allocatable.Cpu().String()
		fields["memory_allocatable"] = node.Status.Allocatable.Memory().String()
		fields["pods_allocatable"] = node.Status.Allocatable.Pods().String()
	}
	
	return fields, nil
}

// extractNamespaceFields extracts fields equivalent to "kubectl get namespaces"
// Columns: NAME, STATUS, AGE
func (t *ResourceSpecificTransformer) extractNamespaceFields(obj runtime.Object) (map[string]interface{}, error) {
	ns, ok := obj.(*corev1.Namespace)
	if !ok {
		return nil, fmt.Errorf("object is not a Namespace")
	}
	
	fields := make(map[string]interface{})
	
	// Status
	fields["status"] = string(ns.Status.Phase)
	
	return fields, nil
}

// extractCommonFields extracts common fields for unknown resource types
func (t *ResourceSpecificTransformer) extractCommonFields(obj runtime.Object) (map[string]interface{}, error) {
	fields := make(map[string]interface{})
	
	// Try to get metav1.Object interface for common metadata
	if metaObj, ok := obj.(metav1.Object); ok {
		fields["generation"] = metaObj.GetGeneration()
		fields["resource_version"] = metaObj.GetResourceVersion()
		
		// Finalizers
		if finalizers := metaObj.GetFinalizers(); len(finalizers) > 0 {
			fields["finalizers"] = finalizers
		}
		
		// Owner references
		if owners := metaObj.GetOwnerReferences(); len(owners) > 0 {
			var ownerInfo []map[string]interface{}
			for _, owner := range owners {
				ownerInfo = append(ownerInfo, map[string]interface{}{
					"kind": owner.Kind,
					"name": owner.Name,
					"uid":  string(owner.UID),
				})
			}
			fields["owners"] = ownerInfo
		}
	}
	
	return fields, nil
}

// Helper functions for other resource types
func (t *ResourceSpecificTransformer) extractReplicaSetFields(obj runtime.Object) (map[string]interface{}, error) {
	rs, ok := obj.(*appsv1.ReplicaSet)
	if !ok {
		return nil, fmt.Errorf("object is not a ReplicaSet")
	}
	
	fields := make(map[string]interface{})
	fields["ready_replicas"] = rs.Status.ReadyReplicas
	fields["replicas"] = rs.Status.Replicas
	fields["desired_replicas"] = *rs.Spec.Replicas
	fields["ready"] = fmt.Sprintf("%d/%d", rs.Status.ReadyReplicas, *rs.Spec.Replicas)
	
	return fields, nil
}

func (t *ResourceSpecificTransformer) extractDaemonSetFields(obj runtime.Object) (map[string]interface{}, error) {
	ds, ok := obj.(*appsv1.DaemonSet)
	if !ok {
		return nil, fmt.Errorf("object is not a DaemonSet")
	}
	
	fields := make(map[string]interface{})
	fields["desired_scheduled"] = ds.Status.DesiredNumberScheduled
	fields["current_scheduled"] = ds.Status.CurrentNumberScheduled
	fields["ready"] = ds.Status.NumberReady
	fields["up_to_date"] = ds.Status.UpdatedNumberScheduled
	fields["available"] = ds.Status.NumberAvailable
	
	return fields, nil
}

func (t *ResourceSpecificTransformer) extractStatefulSetFields(obj runtime.Object) (map[string]interface{}, error) {
	ss, ok := obj.(*appsv1.StatefulSet)
	if !ok {
		return nil, fmt.Errorf("object is not a StatefulSet")
	}
	
	fields := make(map[string]interface{})
	fields["ready_replicas"] = ss.Status.ReadyReplicas
	fields["replicas"] = ss.Status.Replicas
	fields["desired_replicas"] = *ss.Spec.Replicas
	fields["ready"] = fmt.Sprintf("%d/%d", ss.Status.ReadyReplicas, *ss.Spec.Replicas)
	
	return fields, nil
}

func (t *ResourceSpecificTransformer) extractConfigMapFields(obj runtime.Object) (map[string]interface{}, error) {
	cm, ok := obj.(*corev1.ConfigMap)
	if !ok {
		return nil, fmt.Errorf("object is not a ConfigMap")
	}
	
	fields := make(map[string]interface{})
	fields["data_count"] = len(cm.Data)
	fields["binary_data_count"] = len(cm.BinaryData)
	
	return fields, nil
}

func (t *ResourceSpecificTransformer) extractSecretFields(obj runtime.Object) (map[string]interface{}, error) {
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		return nil, fmt.Errorf("object is not a Secret")
	}
	
	fields := make(map[string]interface{})
	fields["type"] = string(secret.Type)
	fields["data_count"] = len(secret.Data)
	
	return fields, nil
}

func (t *ResourceSpecificTransformer) extractIngressFields(obj runtime.Object) (map[string]interface{}, error) {
	ingress, ok := obj.(*networkingv1.Ingress)
	if !ok {
		return nil, fmt.Errorf("object is not an Ingress")
	}
	
	fields := make(map[string]interface{})
	
	// Hosts
	var hosts []string
	for _, rule := range ingress.Spec.Rules {
		if rule.Host != "" {
			hosts = append(hosts, rule.Host)
		}
	}
	fields["hosts"] = hosts
	
	// Load balancer ingress
	var addresses []string
	for _, lb := range ingress.Status.LoadBalancer.Ingress {
		if lb.IP != "" {
			addresses = append(addresses, lb.IP)
		}
		if lb.Hostname != "" {
			addresses = append(addresses, lb.Hostname)
		}
	}
	fields["addresses"] = addresses
	
	return fields, nil
}

func (t *ResourceSpecificTransformer) extractPVFields(obj runtime.Object) (map[string]interface{}, error) {
	pv, ok := obj.(*corev1.PersistentVolume)
	if !ok {
		return nil, fmt.Errorf("object is not a PersistentVolume")
	}
	
	fields := make(map[string]interface{})
	fields["capacity"] = pv.Spec.Capacity.Storage().String()
	fields["access_modes"] = pv.Spec.AccessModes
	fields["reclaim_policy"] = string(pv.Spec.PersistentVolumeReclaimPolicy)
	fields["status"] = string(pv.Status.Phase)
	fields["claim"] = pv.Spec.ClaimRef
	
	return fields, nil
}

func (t *ResourceSpecificTransformer) extractPVCFields(obj runtime.Object) (map[string]interface{}, error) {
	pvc, ok := obj.(*corev1.PersistentVolumeClaim)
	if !ok {
		return nil, fmt.Errorf("object is not a PersistentVolumeClaim")
	}
	
	fields := make(map[string]interface{})
	fields["status"] = string(pvc.Status.Phase)
	fields["volume"] = pvc.Spec.VolumeName
	fields["access_modes"] = pvc.Spec.AccessModes
	
	if pvc.Spec.Resources.Requests != nil {
		if storage, ok := pvc.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
			fields["requested_storage"] = storage.String()
		}
	}
	
	if pvc.Status.Capacity != nil {
		if storage, ok := pvc.Status.Capacity[corev1.ResourceStorage]; ok {
			fields["capacity"] = storage.String()
		}
	}
	
	return fields, nil
}