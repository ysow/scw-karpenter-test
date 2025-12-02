# Contrôleur Karpenter pour Scaleway GPU

Ce projet est un contrôleur Kubernetes personnalisé conçu pour intégrer [Karpenter](https://karpenter.sh/) avec [Scaleway](https://www.scaleway.com/), permettant à Karpenter de provisionner dynamiquement des instances GPU Scaleway en réponse à la demande de vos charges de travail.

Lorsque Karpenter doit provisionner un nœud GPU, il crée une ressource `NodeClaim`. Ce contrôleur surveille ces `NodeClaims` et, s'ils sont configurés pour le type de capacité `scaleway-gpu`, il se charge de créer l'instance correspondante via l'API Scaleway.

## Fonctionnalités

- **Provisionnement automatique** : Crée des instances GPU Scaleway lorsque des `NodeClaims` correspondants apparaissent.
- **Sélection dynamique du type d'instance** : Lit le label `karpenter.sh/instance-type` sur le `NodeClaim` pour provisionner le type de GPU demandé (ex: L4, L40s, 3070).
- **Configuration par `cloud-init`** : Utilise un script `cloud-init` pour configurer le nœud au démarrage et le joindre automatiquement au cluster Kubernetes.
- **Sécurisé** : Construit sur une image Docker `distroless/static` pour une surface d'attaque minimale.
- **Flexible** : Le mappage des types d'instance et le script `cloud-init` sont facilement personnalisables.

---

## Tutoriel de Déploiement

Suivez ces étapes pour déployer et utiliser le contrôleur dans votre cluster.

### Pré-requis

- Un cluster Kubernetes fonctionnel.
- `kubectl` configuré pour accéder à votre cluster.
- Karpenter installé dans votre cluster.
- Un compte Scaleway avec des clés d'API (`SCW_ACCESS_KEY`, `SCW_SECRET_KEY`, `SCW_DEFAULT_PROJECT_ID`).
- Docker installé localement pour construire l'image.
- Go (version 1.21+) installé localement.

### Étape 1 : Cloner le projet

```bash
git clone https://github.com/votre-repo/scw-karpenter.git
cd scw-karpenter
```

### Étape 2 : Configurer les identifiants Scaleway

Le contrôleur a besoin de vos clés d'API Scaleway pour fonctionner. Le fichier `deploy.yaml` contient un template de Secret pour les stocker.

1.  Ouvrez le fichier `deploy.yaml`.
2.  Localisez la section du `Secret` `scaleway-credentials`.
3.  Remplacez les placeholders `<...>` par vos propres clés et projet ID.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: scaleway-credentials
  namespace: default
type: Opaque
stringData:
  SCW_ACCESS_KEY: "<VOTRE_SCW_ACCESS_KEY>"
  SCW_SECRET_KEY: "<VOTRE_SCW_SECRET_KEY>"
  SCW_DEFAULT_PROJECT_ID: "<VOTRE_SCW_DEFAULT_PROJECT_ID>"
  SCW_DEFAULT_REGION: "fr-par"
  SCW_DEFAULT_ZONE: "fr-par-1"
```

### Étape 3 : Construire et Pousser l'image Docker

1.  Construisez l'image Docker en utilisant le `Dockerfile` fourni :
    ```bash
    docker build -t votre-registre/scw-karpenter:latest .
    ```

2.  Poussez l'image vers votre registre de conteneurs (Docker Hub, GCR, etc.) :
    ```bash
    docker push votre-registre/scw-karpenter:latest
    ```

3.  Mettez à jour le nom de l'image dans `deploy.yaml`. Localisez la section `Deployment` et changez la ligne `image` :
    ```yaml
    # ... dans le Deployment scw-karpenter-controller
    spec:
      containers:
      - name: controller
        image: votre-registre/scw-karpenter:latest # <-- Mettez à jour ici
    # ...
    ```

### Étape 4 : Déployer le contrôleur

Appliquez le fichier `deploy.yaml` pour créer le `Secret`, les `ClusterRole/Binding` et le `Deployment` du contrôleur.

```bash
kubectl apply -f deploy.yaml
```

Vérifiez que le pod du contrôleur est en cours d'exécution :
```bash
kubectl get pods -l app=scw-karpenter
```

### Étape 5 : Configurer un Provisioner Karpenter

Créez un `Provisioner` Karpenter qui cible notre type de capacité personnalisé `scaleway-gpu`.

Créez un fichier `karpenter-provisioner.yaml` :
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
  # Définissez les types d'instances que vous autorisez
  # Ces noms doivent correspondre à ceux dans la fonction getCommercialType
  # de controller.go (ex: "l4", "l40s").
  limits:
    resources:
      cpu: 1000
      memory: 1000Gi
  provider:
    # Le champ provider est requis par Karpenter,
    # mais notre contrôleur s'occupe de la logique.
    # Nous le laissons vide.
    {}
  ttlSecondsAfterEmpty: 30
```

Appliquez-le :
```bash
kubectl apply -f karpenter-provisioner.yaml
```

### Étape 6 : Tester le provisionnement d'un GPU

Maintenant, créez une charge de travail qui demande une ressource GPU. Karpenter interceptera cette demande et créera un `NodeClaim`.

Créez un fichier `test-pod.yaml` :
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
  # Le pod doit explicitement tolérer le taint des NodeClaims
  tolerations:
  - key: "karpenter.sh/capacity-type"
    operator: "Exists"
  # Spécifiez le type d'instance GPU souhaité
  nodeSelector:
    karpenter.sh/instance-type: l4
```

Déployez le pod :
```bash
kubectl apply -f test-pod.yaml
```

### Étape 7 : Vérifier le résultat

1.  **Regardez les logs de votre contrôleur**. Vous devriez voir des messages indiquant qu'il a reçu un `NodeClaim` et qu'il crée une instance Scaleway.
    ```bash
    kubectl logs -f -l app=scw-karpenter
    ```
    Vous devriez voir une ligne comme :
    `INFO   creating scaleway instance with cloud-init   {"commercialType": "GPU-L4-S"}`

2.  **Vérifiez la création du `NodeClaim`**.
    ```bash
    kubectl get nodeclaims
    ```

3.  **Vérifiez l'arrivée du nouveau nœud**. Après quelques minutes, l'instance Scaleway démarrera, exécutera le script `cloud-init` et rejoindra le cluster.
    ```bash
    kubectl get nodes
    ```
    Un nouveau nœud provisionné par Karpenter devrait apparaître.

---

## Personnalisation

### Ajouter des types d'instance GPU

Pour supporter d'autres types de GPU Scaleway, modifiez la fonction `getCommercialType` dans le fichier `controller.go` en ajoutant une nouvelle entrée dans `instanceTypeMap`.

### Modifier le script de démarrage

Le script `cloud-init` est généré par la fonction `generateUserData` dans `utils.go`. Vous pouvez modifier cette fonction pour changer la manière dont les nœuds sont configurés au démarrage. N'oubliez pas de remplacer le placeholder `<cluster-endpoint>` par le véritable endpoint de votre API server Kubernetes.
