package main

import (
	"log"
	"net/http"

	"github.com/boltrunner/backend/internal/api"
	"github.com/boltrunner/backend/internal/store/memstore"
)

func main() {
	s := api.NewServer(memstore.NewTestStore())
	log.Println("listening on :8080")
	if err := http.ListenAndServe(":8080", s.Router()); err != nil {
		log.Fatal(err)
	}
}
