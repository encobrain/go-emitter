package emitter

type EmitFlag uint8

const (
	AtomicStatus EmitFlag = 1<<iota
	Sticky                = 1<<iota|AtomicStatus
	StickyCount           = 1<<iota|Sticky|AtomicStatus

)
