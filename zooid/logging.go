package zooid

import "log"

func Debugf(format string, args ...interface{}) {
	if DebugEnabled() {
		log.Printf("[debug] "+format, args...)
	}
}
