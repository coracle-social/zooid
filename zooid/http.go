package zooid

import (
	"log"
	"net/http"
)

func ServeHTTP(w http.ResponseWriter, r *http.Request) {
	relay, err := GetRelay(r.Host)
	if err != nil {
		log.Printf("Failed to load relay config for hostname %s: %v", r.Host, err)
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	relay.ServeHTTP(w, r)
}
