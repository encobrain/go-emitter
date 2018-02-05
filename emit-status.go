package emitter

import "sync"

type EmitStatus struct {
	Sent 	int64
	Skipped int64
	Pending int64

	sync.Mutex
}