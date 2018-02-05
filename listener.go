package emitter

import "sync"

type listener struct {
	pattern    string
	patternID  string
	onFlags    OnFlag
	ch         chan Event
	used       sync.WaitGroup
	count      int64
	middlewars []func(*Event)
}