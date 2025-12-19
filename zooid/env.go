package zooid

import (
	"os"
	"strings"
	"sync"
)

var (
	env     map[string]string
	envOnce sync.Once
)

func Env(k string, fallback ...string) (v string) {
	envOnce.Do(func() {
		env = make(map[string]string)

		env["PORT"] = "3334"
		env["DATA"] = "./data"
		env["MEDIA"] = "./media"
		env["CONFIG"] = "./config"

		for _, item := range os.Environ() {
			parts := strings.SplitN(item, "=", 2)
			env[parts[0]] = parts[1]
		}
	})

	v = env[k]

	if v == "" && len(fallback) > 0 {
		v = fallback[0]
	}

	return v
}

func DebugEnabled() bool {
	switch strings.ToLower(Env("DEBUG")) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}
