package emitter

import "sync"

type Event struct {
	Topic 		string
	Args  		[]interface{}

	emitFlags   EmitFlag
	status      *EmitStatus
	statusUpdCh chan bool
	cancelCh    chan bool
	stickyCount int64
	used        *sync.WaitGroup
	
	flags 		OnFlag

	holdStatus  *sync.WaitGroup
}

func (e *Event) ReleaseStatus () {
 	if e.holdStatus != nil { e.holdStatus.Done() }
}

func (e *Event) Void () {
	e.flags |= Void
}