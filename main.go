package main

import (
	"log"

	"github.com/scaleway/scaleway-sdk-go/scw"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func main() {
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	// Initialize Scaleway client
	scwClient, err := scw.NewClient(scw.WithEnv())
	if err != nil {
		log.Fatal("unable to create scaleway client: ", err)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{})
	if err != nil {
		log.Fatal("unable to start manager: ", err)
	}

	if err := (&ScalewayReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		scwClient: scwClient,
	}).SetupWithManager(mgr); err != nil {
		log.Fatal("unable to create controller: ", err)
	}

	log.Println("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Fatal("problem running manager: ", err)
	}
}
