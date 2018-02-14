package emitter

import (
	"sync"
	"fmt"
	"strings"
	"strconv"
	"regexp"
)

type Emitter struct {
	// format:  (\d+:{pattern}\n)*
	patterns 	 	string
	patternID 	 	uint64
	channels 	 	map[string]chan Event
	listeners 	 	map[chan Event]*listener
	stickyEvents 	[]*Event
	topicReCache  	map[string]*regexp.Regexp

	mu 			 sync.Mutex
}

// Sunscribes for events by pattern
// pattern format: [^\n]+
// close(channel) === e.Off("*", channel)
// channel can be closed by use e.Off(pattern)
func (e *Emitter) On (pattern string, onFlags ...interface{}) (channel chan Event) {
	if strings.Index(pattern,"\n") >= 0 { panic(fmt.Errorf("Incorrect pattern: %s", pattern)) }

	e.mu.Lock()

	channel = make(chan Event)

	var ln = &listener{
		count:		-1,
		pattern:	pattern,
		patternID:  strconv.FormatUint(e.patternID, 10),
		ch:			channel,
	}

	e.patternID++

	i := 0
	l := len(onFlags)

	for i<l {
		f := onFlags[i].(OnFlag)
		i++

		ln.onFlags |= f

		switch f {
			case Count:
			 	ln.count = int64(onFlags[i].(int))
			 	i++
			case Middleware:
				ln.middlewars = append(ln.middlewars, onFlags[i].(func(*Event)) )
				i++
		}
	}
	
	if e.channels == nil { e.channels = map[string]chan Event{} }
	if e.listeners == nil { e.listeners = map[chan Event]*listener{} }

	patternStr := ln.patternID + ":" + pattern + "\n"

	e.patterns += patternStr
	e.channels[ln.patternID] = channel
	e.listeners[channel] = ln

	lns := []*listener{ln}

	for _,ev := range e.stickyEvents {
		if e.getTopicRe(ev.Topic).MatchString(patternStr) {
			go e.emitEvent(ev, lns)
		}
	}
	
	e.mu.Unlock()

	return
}

// Subscribes for one event by pattern
// More: see On(...)
func (e *Emitter) Once (pattern string, onFlags ...interface{}) (channel chan Event) {
	return e.On(pattern, append([]interface{}{Count,1}, onFlags...)...)
}


func safeClose (ch chan Event) {
	defer func() { recover() }()
	close(ch)
}

// Unsibscribe lisener by pattern and/or channels
func (e *Emitter) Off (pattern string, channels ...chan Event) {
	e.mu.Lock()

	if len(channels)>0 {
		for _,ch := range channels {
			l := e.listeners[ch]

			if l != nil {
				e.patterns = strings.Replace(e.patterns, l.patternID + ":" + l.pattern + "\n", "", 1)
				delete(e.listeners, ch)
				delete(e.channels, l.patternID)
				safeClose(l.ch)
			}
		}

		e.mu.Unlock()

		return 
	}

	if strings.Index(pattern,"\n") >= 0 { panic(fmt.Errorf("Incorrect pattern: %s", pattern)) }

	patternRes := strings.Replace(regexp.QuoteMeta(pattern), "\\*", "[^.]+", -1)

	patternRe,err := regexp.Compile("(?m)^(\\d+):" + patternRes + "\n")

	if err != nil { panic(fmt.Errorf("Incorrect pattern: %s", pattern)) }

	ids := patternRe.FindAllStringSubmatch(e.patterns, -1)

	for _,id := range ids {
		ch := e.channels[id[1]]
		l := e.listeners[ch]
		e.patterns = strings.Replace(e.patterns, id[0], "", 1)
		
		delete(e.listeners, ch)
		delete(e.channels, id[1])
		safeClose(l.ch)
	}

	e.mu.Unlock()
}

// Emits event
func (e *Emitter) Emit (topic string, emitFlagsArgs ...interface{}) (statusCh chan EmitStatus) {
	var event = &Event{
		Emitter: 	 e,
		Topic:       topic,
		status:      &EmitStatus{},
		statusUpdCh: make(chan bool,1),
		cancelCh:    make(chan bool),
		stickyCount: -1,
		used:        &sync.WaitGroup{},
	}

	i := 0
	l := len(emitFlagsArgs)

	for i<l {
		f,ok := emitFlagsArgs[i].(EmitFlag)

		if !ok { break }

		i++

		event.emitFlags |= f

		switch f {
			case StickyCount:
				event.stickyCount = int64(emitFlagsArgs[i].(int))
				i++
		}
	}

	if i!=0 { emitFlagsArgs = emitFlagsArgs[i:] }

	event.Args = emitFlagsArgs

	isSticky := event.emitFlags & Sticky != 0

	if isSticky {
	  	e.mu.Lock()
	  	e.stickyEvents = append(e.stickyEvents, event)
	  	e.mu.Unlock()
	}

	statusCh = make(chan EmitStatus)

	go func() {
		defer func() {
			recover()
			
			if isSticky {
				e.mu.Lock()
				for i,ev := range e.stickyEvents {
					if ev == event {
						e.stickyEvents = append(e.stickyEvents[:i], e.stickyEvents[i+1:]...)
						break
					}
				}
				e.mu.Unlock()
			}
		}()

		defer close(statusCh)
		defer close(event.statusUpdCh)
		defer close(event.cancelCh)

		upd:
		for {
			select {
				case <-event.statusUpdCh:
				case <-statusCh:
					break upd
				case <-event.cancelCh:
					break upd
			}
			
			select {
				case statusCh<- *event.status:
				default:
			}
		}
	}()

	go e.emitEvent(event, nil)

	return
}

// Emits stiky event
func (e *Emitter) EmitSticky (topic string, args ...interface{}) (status chan EmitStatus) {
	return e.Emit(topic, append([]interface{}{Sticky}, args...)...)
}

func (e *Emitter) getTopicRe (topic string) (topicRe *regexp.Regexp) {
	topicRe = e.topicReCache[topic]

	if topicRe == nil {
		if e.topicReCache == nil { e.topicReCache = map[string]*regexp.Regexp{} }

		topicReParts := strings.Split(regexp.QuoteMeta(topic), "\\.")

		for i,part := range topicReParts {
			topicReParts[i] = "(?:"+part+"|\\*)"
		}

		topicRe = regexp.MustCompile("(?m)^(\\d+):"+strings.Join(topicReParts, "\\.")+"$")

		e.topicReCache[topic] = topicRe
	}

	return 
}

func (e *Emitter) getListeners (topic string) (listeners []*listener) {
	e.mu.Lock()

	topicRe := e.getTopicRe(topic)

	ids := topicRe.FindAllStringSubmatch(e.patterns,-1)

	for _,id := range ids {
		ch := e.channels[id[1]]

		listeners = append(listeners, e.listeners[ch])
	}

	e.mu.Unlock()

	return
}

func (e *Emitter) emitEvent (rootEvent *Event, listeners []*listener) {
	defer func() { recover() }()

	rootEvent.used.Add(1)
	defer rootEvent.used.Add(-1)

	event := *rootEvent

	if listeners == nil { listeners = e.getListeners(event.Topic) }

	for _,l := range listeners {
		for _,fn := range l.middlewars { fn(&event) }

		isVoid := event.flags & Void == Void

		if isVoid { return }
	}

	isSticky := rootEvent.emitFlags & Sticky == Sticky
	isAtomicStatus := rootEvent.emitFlags & AtomicStatus == AtomicStatus

	if !isSticky { defer close(rootEvent.cancelCh) }

	if !isAtomicStatus {
		event.statusUpdCh = make(chan bool,1)
		defer close(event.statusUpdCh)

		defer func() {
			select {
				case <-event.statusUpdCh:
					select {
						case rootEvent.statusUpdCh<-true:
						default:
					}
				default:
			}
		}()
	}

	sending := &sync.WaitGroup{}

	e.mu.Lock()

	for i := range listeners {
		l := listeners[i]

		isMiddleware := l.onFlags & Middleware == Middleware

		if isMiddleware { continue }

		if rootEvent.stickyCount == 0 { break }

		if l.count == 0 { continue }

		if rootEvent.stickyCount > 0 {
			rootEvent.stickyCount--
			
			if rootEvent.stickyCount == 0 {
				go func() {
					defer func() { recover() }()
					rootEvent.used.Wait()
					close(rootEvent.cancelCh)
				}()
			}
		}

		l.used.Add(1)

		if l.count > 0 {
			l.count--

			if l.count == 0 {
				go func() {
					l.used.Wait()
					e.Off("*", l.ch)
				}()
			}
		}
		                            
		evt := event
		evt.Args = append([]interface{}{}, evt.Args...)

		isHoldStatus := l.onFlags & HoldStatus == HoldStatus

		if isHoldStatus {
			sending.Add(1)
			evt.holdStatus = sending
		}

		sending.Add(1)

		rootEvent.status.Lock()
		rootEvent.status.Pending++
		rootEvent.status.Unlock()

		go func() {
			sent := e.sendEvent(evt, l)
			if !sent { evt.ReleaseStatus() }
			l.used.Done()
			sending.Done()
		}()
	}

	if isAtomicStatus {
		go func() {
			defer func() { recover() }()
			select {
				case rootEvent.statusUpdCh<- true:
				default:
			}
		}()
	}

	e.mu.Unlock()
	
	sending.Wait()
}

func (e *Emitter) sendEvent (event Event, l *listener) (sent bool) {
	defer func() {
		err := recover()
		if err != nil { e.Off("*", l.ch) }
	}()

	isWait := l.onFlags & (Skip|Close) == 0

	if isWait {
		select {
			case <-event.cancelCh:
				return
			case l.ch<- event:
				sent=true
				event.status.Lock()
				event.status.Sent++
				event.status.Pending--
				event.status.Unlock()
		}
	} else {
		select {
			case <-event.cancelCh:
				return
			case l.ch<- event:
				sent=true
				event.status.Lock()
				event.status.Sent++
				event.status.Pending--
				event.status.Unlock()
			default:
				event.status.Lock()
				event.status.Skipped++
				event.status.Pending--
				event.status.Unlock()

				isClose := l.onFlags & Close == Close
				
				if isClose { e.Off("*", l.ch) }
		}
	}

	select {
	 	case event.statusUpdCh<- true:
		default:
	}

	return 
}