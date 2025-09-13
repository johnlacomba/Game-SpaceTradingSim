package main

import (
	"log"
	"net/http"

	srv "github.com/example/space-trader/internal/server"
	"github.com/gorilla/mux"
)

func main() {
	r := mux.NewRouter()

	gs := srv.NewGameServer()

	r.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		gs.HandleWS(w, r)
	})
	// Debug REST
	r.HandleFunc("/rooms", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			gs.HandleListRooms(w, r)
			return
		}
		if r.Method == http.MethodPost {
			gs.HandleCreateRoom(w, r)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})

	addr := ":8080"
	log.Printf("Space Trader backend listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, r))
}
