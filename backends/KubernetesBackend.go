package backends

import (
	"fmt"
	"net"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8s "k8s.io/client-go/kubernetes"
)

const (
	// podAnnotationLock is used to lock pods and prevent assigning multiple connections
	// to the same pod at the same time
	podAnnotationLock = "kramergroup.science.vncd.lock"
)

/*
KubernetesBackend implements a Backend that uses Kubernetes Pods to handle
requests.

Pod creation and management is left to Kubernetes, but the backend factory will
ensure that a pod is only used once at any point in time to handle a connection.
*/
type KubernetesBackend struct {
	podName       string         // The name of the pod handling the connection
	nameSpace     string         // The namespace of the pod handling the connection
	containerPort int            // The port at which the container is listening
	clientset     *k8s.Clientset // The k8s client
}

// CreateKubernetesBackend creates a KubernetesBackend to handle requests. It searches
// the provided 'namespace' for a pod matching 'label' and without 'podAnnotationLock'.
// It then sets the lock to indicate that this pod is currently handling a connection.
func CreateKubernetesBackend(clientset *k8s.Clientset, namespace string, labelSelector string, containerPort int) (Backend, error) {

	// Find a suitable pod
	podList, err := clientset.CoreV1().Pods(namespace).List(metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		return nil, fmt.Errorf("List Pods of namespace[%s] error:%v", namespace, err)
	}
	for _, pod := range podList.Items {
		if _, ok := pod.Annotations[podAnnotationLock]; ok {
			continue // This pod is locked - move on
		} else {
			// Found a pod to handle the connection. Lock it and store info in KubernetesBackend
			pod.Annotations[podAnnotationLock] = "yes"
			_, err = clientset.CoreV1().Pods(namespace).Update(&pod)
			if err != nil {
				return nil, fmt.Errorf("Error locking pod [%s] in namespace [%s]", pod.ObjectMeta.Name, pod.ObjectMeta.Namespace)
			}
			return &KubernetesBackend{
				podName:       pod.ObjectMeta.Name,
				nameSpace:     pod.ObjectMeta.Namespace,
				containerPort: containerPort,
				clientset:     clientset,
			}, nil
		}
	}
	return nil, fmt.Errorf("No available pod in namespace [%s]", namespace)
}

// GetTarget returns the TCP address of the handling Pod
func (b *KubernetesBackend) GetTarget() (*net.TCPAddr, error) {
	pod, err := b.getPod()
	if err != nil {
		return nil, err
	}
	addr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf("%s:%d", pod.Status.PodIP, b.containerPort))
	return addr, err
}

// Terminate removes the lock from the pod and makes it available for
// scheduling again
func (b *KubernetesBackend) Terminate() {
	pod, err := b.getPod()
	if err != nil {
		fmt.Printf("Error releasing pod lock. Cannot find pod [%s] in namespace [%s]", b.podName, b.nameSpace)
		return
	}
	delete(pod.ObjectMeta.Annotations, podAnnotationLock)
	_, err = b.clientset.CoreV1().Pods(b.nameSpace).Update(pod)
	if err != nil {
		fmt.Println("Error updating pod " + b.podName + " in namespace " + b.nameSpace)
	}
	fmt.Printf("Released lock from pod [%s] in namespace [%s]\n", b.podName, b.nameSpace)
}

func (b *KubernetesBackend) getPod() (*v1.Pod, error) {
	// config, err := rest.InClusterConfig()
	// clientset, err := kubernetes.NewForConfig(config)
	return b.clientset.CoreV1().Pods(b.nameSpace).Get(b.podName, metav1.GetOptions{})
}
