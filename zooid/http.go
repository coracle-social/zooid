package zooid

import (
	"log"
	"net/http"
)

func ServeHTTP(w http.ResponseWriter, r *http.Request) {
	instance, err := GetInstance(r.Host)
	if err != nil {
		log.Printf("Failed to load config for hostname %s: %v", r.Host, err)
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	instance.Relay.ServeHTTP(w, r)
}
