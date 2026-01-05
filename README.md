# Karpenter Controller for Scaleway GPU

This project is a custom Kubernetes controller designed to integrate [Karpenter](https://karpenter.sh/) with [Scaleway](https://www.scaleway.com/), allowing Karpenter to dynamically provision Scaleway GPU instances in response to your workload demands.

When Karpenter needs to provision a GPU node, it creates a `NodeClaim` resource. This controller monitors these `NodeClaims` and, if they are configured for the `scaleway-gpu` capacity type, it handles the creation of the corresponding instance via the Scaleway API.

## Features

- **Automatic Provisioning**: Creates Scaleway GPU instances when corresponding `NodeClaims` appear.
- **Dynamic Instance Type Selection**: Reads the `karpenter.sh/instance-type` label on the `NodeClaim` to provision the requested GPU type (e.g., L4, L40s, 3070).
- **`cloud-init` Configuration**: Uses a `cloud-init` script to configure the node on startup and automatically join it to the Kubernetes cluster.
- **Secure**: Built on a `distroless/static` Docker image for a minimal attack surface.
- **Flexible**: The instance type mapping and `cloud-init` script are easily customizable.

---

## Deployment Tutorial

Follow these steps to deploy and use the controller in your cluster.

### Prerequisites

- A functional Kubernetes cluster.
- `kubectl` configured to access your cluster.
- Karpenter installed in your cluster.
- A Scaleway account with API keys (`SCW_ACCESS_KEY`, `SCW_SECRET_KEY`, `SCW_DEFAULT_PROJECT_ID`).
- Docker installed locally to build the image.
- Go (version 1.21+) installed locally.

### Step 1: Clone the project

```bash
git clone https://github.com/your-repo/scw-karpenter.git
cd scw-karpenter
```

### Step 2: Configure Scaleway credentials

The controller needs your Scaleway API keys to work. The `deploy.yaml` file contains a Secret template to store them.

1.  Open the `deploy.yaml` file.
2.  Locate the `scaleway-credentials` `Secret` section.
3.  Replace the `<...>` placeholders with your own keys and project ID.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: scaleway-credentials
  namespace: default
type: Opaque
stringData:
  SCW_ACCESS_KEY: "<YOUR_SCW_ACCESS_KEY>"
  SCW_SECRET_KEY: "<YOUR_SCW_SECRET_KEY>"
  SCW_DEFAULT_PROJECT_ID: "<YOUR_SCW_DEFAULT_PROJECT_ID>"
  SCW_DEFAULT_REGION: "fr-par"
  SCW_DEFAULT_ZONE: "fr-par-1"
```

### Step 3: Build and Push the Docker image

1.  Build the Docker image using the provided `Dockerfile`:
    ```bash
    docker build -t your-registry/scw-karpenter:latest .
    ```

2.  Push the image to your container registry (Docker Hub, GCR, etc.):
    ```bash
    docker push your-registry/scw-karpenter:latest
    ```

3.  Update the image name in `deploy.yaml`. Locate the `Deployment` section and change the `image` line:
    ```yaml
    # ... in the scw-karpenter-controller Deployment
    spec:
      containers:
      - name: controller
        image: your-registry/scw-karpenter:latest # <-- Update here
    # ...
    ```

### Step 4: Deploy the controller

Apply the `deploy.yaml` file to create the `Secret`, `ClusterRole/Binding`, and the controller's `Deployment`.

```bash
kubectl apply -f deploy.yaml
```

Check that the controller pod is running:
```bash
kubectl get pods -l app=scw-karpenter
```

### Step 5: Configure a Karpenter Provisioner

Create a Karpenter `Provisioner` that targets our custom `scaleway-gpu` capacity type.

Create a `karpenter-provisioner.yaml` file:
```yaml
apiVersion: karpenter.sh/v1alpha5
kind: Provisioner
metadata:
  name: scaleway-gpu
spec:
  requirements:
    - key: karpenter.sh/capacity-type
      operator: In
      values: ["scaleway-gpu"]
  # Define the instance types you allow
  # These names must match those in the getCommercialType function
  # in controller.go (e.g., "l4", "l40s").
  limits:
    resources:
      cpu: 1000
      memory: 1000Gi
  provider:
    # The provider field is required by Karpenter,
    # but our controller handles the logic.
    # We leave it empty.
    {}
  ttlSecondsAfterEmpty: 30
```

Apply it:
```bash
kubectl apply -f karpenter-provisioner.yaml
```

### Step 6: Test GPU provisioning

Now, create a workload that requests a GPU resource. Karpenter will intercept this request and create a `NodeClaim`.

Create a `test-pod.yaml` file:
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: gpu-test
spec:
  containers:
  - name: cuda-container
    image: nvidia/cuda:11.4.2-base-ubuntu20.04
    command: ["/bin/bash", "-c", "--"]
    args: ["while true; do sleep 60; done;"]
    resources:
      requests:
        nvidia.com/gpu: "1"
      limits:
        nvidia.com/gpu: "1"
  # The pod must explicitly tolerate the NodeClaims taint
  tolerations:
  - key: "karpenter.sh/capacity-type"
    operator: "Exists"
  # Specify the desired GPU instance type
  nodeSelector:
    karpenter.sh/instance-type: l4
```

Deploy the pod:
```bash
kubectl apply -f test-pod.yaml
```

### Step 7: Check the result

1.  **Look at your controller's logs**. You should see messages indicating that it has received a `NodeClaim` and is creating a Scaleway instance.
    ```bash
    kubectl logs -f -l app=scw-karpenter
    ```
    You should see a line like:
    `INFO   creating scaleway instance with cloud-init   {"commercialType": "GPU-L4-S"}`

2.  **Check the `NodeClaim` creation**.
    ```bash
    kubectl get nodeclaims
    ```

3.  **Check the arrival of the new node**. After a few minutes, the Scaleway instance will start, execute the `cloud-init` script, and join the cluster.
    ```bash
    kubectl get nodes
    ```
    A new node provisioned by Karpenter should appear.

---

## Customization

### Add GPU instance types

To support other types of Scaleway GPUs, modify the `getCommercialType` function in the `controller.go` file by adding a new entry to the `instanceTypeMap`.

### Modify the startup script

The `cloud-init` script is generated by the `generateUserData` function in `utils.go`. You can modify this function to change how nodes are configured on startup. Don't forget to replace the `<cluster-endpoint>` placeholder with the actual endpoint of your Kubernetes API server.
