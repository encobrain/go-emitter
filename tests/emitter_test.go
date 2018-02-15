package tests

import (
	"testing"
	"time"

	. "github.com/encobrain/go-emitter"
)

func TestSimpleEmit (T *testing.T) {
	var em Emitter

	ch := em.On("test")

	em.Emit("test")

	<-ch
}

func TestArgs (T *testing.T) {
	var em Emitter

	ch := em.On("test")

	em.Emit("test",1,2,3)

	e := <-ch

	if len(e.Args) != 3 {
		T.Fatalf("Incorrect len: %d", len(e.Args))
	}

	if e.Args[0] != 1 || e.Args[1] != 2 || e.Args[2] != 3 {
		T.Fatalf("Incorrect values: %v", e.Args)
	}
}

func TestUnchangedArgs (T *testing.T) {
	var em Emitter

	ch1 := em.On("test")
	ch2 := em.On("test")

	em.Emit("test",1,2,3)

	e := <-ch1

	e.Args[0]=3

	e = <-ch2

	if e.Args[0] != 1 {
		T.Fatalf("Args are changed: %v", e.Args)
	}
}

func TestOnMiddleware (T *testing.T) {
	var em Emitter

	done := make(chan int)
	
	em.On("test", Middleware, func (*Event) { done<-1 })

	em.Emit("test")

	<-done
}

func TestOnOnce (T *testing.T) {
	var em Emitter

	ch1 := em.Once("test")

	em.Emit("test")
	em.Emit("test")

	count := 0

	for range ch1 {
		count++
	}

	if count != 1 {
		T.Fatalf("Incorrect count: %d", count)
	}
}

func TestOnCountFlag (T *testing.T) {
	var em Emitter

	ch1 := em.On("test", Count, 2)

	em.Emit("test")
	em.Emit("test")
	em.Emit("test")

	count := 0

	for range ch1 {
		count++
	}

	if count != 2 {
		T.Fatalf("Incorrect count: %d", count)
	}
}

func TestOnVoidFlag (T *testing.T) {
	var em Emitter

	ch1 := em.On("test")
	ch2 := em.On("test")
	
	em.On("test", Middleware, func(event *Event) { if event.Args[0]==1 { event.Void() } })

	em.Emit("test", 1)
	em.Emit("test", 2)

	if (<-ch1).Args[0] == 1 || (<-ch2).Args[0] == 1 {
		T.Fatalf("Void not work")
	}

}

func TestOnSkipFlag (T *testing.T) {
	var em Emitter

	ch1 := em.On("test")
	ch2 := em.On("test", Skip)

	go func () { <-ch1 }()

	<-em.Emit("test", 1)
	em.Emit("test", 2)
	e := <-ch2

	if e.Args[0] != 2 {
		T.Fatalf("Not skipped: %+v", e.Args)
	}
}

func TestOnCloseFlag (T *testing.T) {
	var em Emitter

	ch1 := em.On("test")
	ch2 := em.On("test", Close)

	go func() { <-ch1 }()

	<-em.Emit("test", 1)
	
	em.Emit("test", 2)
	e,ok := <-ch2

	if ok {
		T.Fatalf("Not closed: %v", e)
	}
}

func TestOnHoldStatusFlag (T *testing.T) {
	var em Emitter

	ch1 := em.On("test", HoldStatus)

	holdDelta := time.Millisecond*10

	go func() {
		e := <-ch1
		time.Sleep(holdDelta)
		e.ReleaseStatus()
	}()

	t := time.Now()
	<-em.Emit("test")

	delta := time.Now().Sub(t)

	if delta < holdDelta {
		T.Fatalf("Incorrect hold time: %v", delta)
	}
}

func TestOnByPattern (T *testing.T) {
	var em Emitter

	ch1 := em.On("test.*.klm")
	ch2 := em.On("*.*.klm")

	em.Emit("test.abc.klm", 1)

	e := <-ch1

	if e.Args[0] != 1 {
		T.Fatalf("Incorrect args: %v", e.Args)
	}

	e = <-ch2

	if e.Args[0] != 1 {
		T.Fatalf("Incorrect args: %v", e.Args)
	}

	em.Emit("test.cde.klm", 2)

	e = <-ch1

	if e.Args[0] != 2 {
		T.Fatalf("Incorrect args: %v", e.Args)
	}

	e = <-ch2

	if e.Args[0] != 2 {
		T.Fatalf("Incorrect args: %v", e.Args)
	}
}


func TestEmitStatus (T *testing.T) {
	var em Emitter

	ch1 := em.On("test")
	em.On("test", Skip)
	ch2 := em.On("test")

	go func() {
		<-ch1
	}()

	go func() {
		time.Sleep(time.Millisecond*20)
		<-ch2
	}()

	st := <-em.Emit("test")

	if st.Sent != 2 || st.Skipped != 1 || st.Pending != 0 {
		T.Fatalf("Incorrect status: %+v", st)
	}
}

func TestEmitAtomicStatus (T *testing.T) {
	var em Emitter

	ch1 := em.On("test")
	em.On("test", Skip)
	ch2 := em.On("test")

	go func() {
		<-ch1
	}()

	go func() {
		time.Sleep(time.Millisecond*20)
		<-ch2
	}()

	var st EmitStatus

	for st = range em.Emit("test", AtomicStatus) { }

	if st.Skipped+st.Pending+st.Sent != 3 {
		T.Fatalf("Incorrect status: %+v", st)
	}
}

func TestOff (T *testing.T) {
	var em Emitter

	ch1 := em.On("test")

	em.Emit("test")

	em.Off("*", ch1)

	for range ch1 {
		T.Fatalf("Off not work")
	}
}

func TestOffByPattern (T *testing.T) {
	var em Emitter

	ch1 := em.On("test.abc.klm")
	ch2 := em.On("test.cde.klm")
	ch3 := em.On("test.efg.klm.opq")

	em.Emit("test.abc.klm")
	em.Emit("test.cde.klm")
	em.Emit("test.efg.klm.opq")

	em.Off("test.*.klm")
	
	for range ch1 {
		T.Fatalf("Off not work")
	}

	for range ch2 {
		T.Fatalf("Off not work")
	}

	_,ok := <-ch3

	if !ok {
		T.Fatalf("Off incorrect")
	}
	
	ch1 = em.On("test.abc.klm")
	ch2 = em.On("test.cde.klm")

	em.Emit("test.abc.klm")
	em.Emit("test.cde.klm")

	em.Off("*")

	for range ch1 {
		T.Fatalf("Off not work")
	}

	for range ch2 {
		T.Fatalf("Off not work")
	}

}

func TestEmitSticky (T *testing.T) {
   	var em Emitter

   	<-em.EmitSticky("test")

   	ch1 := em.On("test")
   	ch2 := em.On("test")

   	<-ch1
   	<-ch2
}


func TestEmitCount (T *testing.T) {
	var em  Emitter

	<-em.Emit("test", Count, 2, "val")

	ch1 := em.Once("test")
	ch2 := em.Once("test")

	time.Sleep(time.Millisecond*20)
	e := <-ch1

	if e.Args[0] != "val" {
		T.Fatalf("Incorrect args: %v", e.Args)
	}

	time.Sleep(time.Millisecond*20)
	e = <-ch2

	if e.Args[0] != "val" {
		T.Fatalf("Incorrect args: %v", e.Args)
	}

	ch3 := em.On("test")

	em.Emit("test", "val2")

	e = <-ch3

	if e.Args[0] != "val2" {
		T.Fatalf("Sticky count not work: %v", e.Args)
	}

}

func TestOnAndClose (T *testing.T) {
	var em Emitter

	ch := em.On("test")
	close(ch)

	<-em.Emit("test")
}

func TestCapFlag (T *testing.T) {
 	var em Emitter

 	ch := em.On("test", Cap, 1)

 	<-em.Emit("test", 10)

 	e := <-ch

	if e.Args[0].(int) != 10 {
		T.Fatalf("Incorrect args: %#v", e.Args)
	}
}

func TestMiddlewareClose (T *testing.T) {
	var em Emitter

	ch := em.On("test", Middleware, func(e *Event) {
		T.Errorf("Incorrect work")
	})

	close(ch)

	<-em.Emit("test")
}
