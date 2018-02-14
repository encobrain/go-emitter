package emitter

type Flag uint16

const (
	Cap      Flag 	= 1<<iota
	Count

	Void
	Skip
	Close
	HoldStatus
	Middleware  
	//emit
	AtomicStatus
	Sticky          = 1<<iota|AtomicStatus
)
