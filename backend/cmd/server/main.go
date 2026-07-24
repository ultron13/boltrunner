package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/boltrunner/backend/internal/api"
	"github.com/boltrunner/backend/internal/k8sjob"
	"github.com/boltrunner/backend/internal/store/postgres"
	"github.com/boltrunner/backend/internal/watcher"
)

func buildK8sClient() (kubernetes.Interface, error) {
	if cfg, err := rest.InClusterConfig(); err == nil {
		return kubernetes.NewForConfig(cfg)
	}
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = os.Getenv("HOME") + "/.kube/config"
	}
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(cfg)
}

func main() {
	ctx := context.Background()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is required")
	}
	db, err := postgres.Connect(ctx, dsn)
	if err != nil {
		log.Fatalf("connect to postgres: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	k8sClient, err := buildK8sClient()
	if err != nil {
		log.Fatalf("build k8s client: %v", err)
	}

	jobCfg := k8sjob.Config{
		Namespace:    envOr("BOLTRUNNER_NAMESPACE", "boltrunner"),
		JMeterImage:  envOr("JMETER_IMAGE", "boltrunner/jmeter:local"),
		SidecarImage: envOr("SIDECAR_IMAGE", "boltrunner/sidecar:local"),
		BackendURL:   envOr("BACKEND_URL", "http://boltrunner-backend.boltrunner.svc:8080"),
	}

	w := watcher.New(k8sClient, db, jobCfg.Namespace, 30*time.Second)
	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go w.Run(watchCtx, 2*time.Second)

	s := api.NewServer(db, db, k8sClient, jobCfg)
	log.Println("listening on :8080")
	if err := http.ListenAndServe(":8080", s.Router()); err != nil {
		log.Fatal(err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
