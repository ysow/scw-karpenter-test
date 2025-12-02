package main

import (
	"fmt"
)

// generateUserData creates a cloud-init script for configuring a new node.
func generateUserData(clusterName string, token string) string {
	// In a real scenario, you would get the cluster endpoint dynamically.
	const clusterEndpoint = "<cluster-endpoint>"

	script := `#!/bin/bash
apt-get update
apt-get install -y kubelet kubeadm kubectl

# Join the Kubernetes cluster
`
	script += fmt.Sprintf("kubeadm join --token %s %s\n", token, clusterEndpoint)

	return script
}
