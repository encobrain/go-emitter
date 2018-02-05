package emitter

type OnFlag uint8

const (
	Count 		OnFlag = 1<<iota
	Void
	Skip
	Close
	HoldStatus
	Middleware 		    = 1<<iota|Skip
)