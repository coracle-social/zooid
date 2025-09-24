package zooid

import (
	"os"
	"strings"
	"sync"
)

const (
	AUTH_JOIN   = 28934
	AUTH_INVITE = 28935
)

var (
	env     map[string]string
	envOnce sync.Once
)

func Env(k string, fallback ...string) (v string) {
	envOnce.Do(func() {
		env = make(map[string]string)

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
