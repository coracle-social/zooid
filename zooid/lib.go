package zooid

import (
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
)

var (
	instancesByHost map[string]*Instance
	instancesByName map[string]*Instance
	instancesOnce   sync.Once
	instancesMux    sync.RWMutex
)

func Dispatch(hostname string) (*Instance, bool) {
	instancesMux.RLock()
	defer instancesMux.RUnlock()

	instance, exists := instancesByHost[hostname]

	return instance, exists
}

func Start() {
	dataDir := Env("DATA")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Fatal("Failed to create data directory: %v", err)
	}

	mediaDir := Env("MEDIA")
	if err := os.MkdirAll(mediaDir, 0755); err != nil {
		log.Fatal("Failed to create media directory: %v", err)
	}

	configDir := Env("CONFIG")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		log.Fatal("Failed to create config directory: %v", err)
	}

	instancesOnce.Do(func() {
		instancesByHost = make(map[string]*Instance)
		instancesByName = make(map[string]*Instance)
	})

	entries, err := os.ReadDir(configDir)
	if err != nil {
		log.Fatal("Failed to scan config directory: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		instance, err := MakeInstance(entry.Name())

		if err != nil {
			log.Printf("Failed to make instance for %s: %v", entry.Name(), err)
		} else {
			instancesByHost[instance.Config.Host] = instance
			instancesByName[entry.Name()] = instance
			log.Printf("Loaded %s", entry.Name())
		}
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("Failed to create file watcher: %v", err)
		return
	}

	defer watcher.Close()

	if err := watcher.Add(configDir); err != nil {
		log.Printf("Failed to watch config directory: %v", err)
		return
	}

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			filename := filepath.Base(event.Name)

			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) {
				instancesMux.Lock()

				if instance, exists := instancesByName[filename]; exists {
					instance.Cleanup()

					delete(instancesByHost, instance.Config.Host)
					delete(instancesByName, filename)
				}

				instance, err := MakeInstance(filename)
				if err != nil {
					log.Printf("Failed to reload %s: %v", filename, err)
				} else {
					instancesByHost[instance.Config.Host] = instance
					instancesByName[filename] = instance
					log.Printf("Reloaded %v", filename)
				}

				instancesMux.Unlock()
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}

			log.Printf("File watcher error: %v", err)
		}
	}
}
