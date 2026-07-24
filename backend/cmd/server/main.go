package main

import (
	"log"
	"net/http"

	"k8s.io/client-go/kubernetes/fake"

	"github.com/boltrunner/backend/internal/api"
	"github.com/boltrunner/backend/internal/k8sjob"
	"github.com/boltrunner/backend/internal/store/memstore"
)

func main() {
	cfg := k8sjob.Config{Namespace: "boltrunner", JMeterImage: "boltrunner/jmeter:local", SidecarImage: "boltrunner/sidecar:local", BackendURL: "http://localhost:8080"}
	s := api.NewServer(memstore.NewTestStore(), memstore.NewRunStore(), fake.NewSimpleClientset(), cfg)
	log.Println("listening on :8080")
	if err := http.ListenAndServe(":8080", s.Router()); err != nil {
		log.Fatal(err)
	}
}
