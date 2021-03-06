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
func (e *Emitter) On (pattern string, flags ...interface{}) (channel chan Event) {
	if strings.Index(pattern,"\n") >= 0 { panic(fmt.Errorf("Incorrect pattern: %s", pattern)) }

	e.mu.Lock()

	var ln = &listener{
		count:		-1,
		pattern:	pattern,
		patternID:  strconv.FormatUint(e.patternID, 10),
	}

	channelCap := 0

	e.patternID++

	i := 0
	l := len(flags)

	for i<l {
		f := flags[i].(Flag)
		i++

		switch f {
			case Skip, Close, HoldStatus:
			case Cap:
				channelCap = int(flags[i].(int))
				i++
			case Count:
			 	ln.count = int64(flags[i].(int))
			 	i++
			case Middleware:
				ln.middlewars = append(ln.middlewars, flags[i].(func(*Event)) )
				i++
			default:
			 	panic(fmt.Errorf("Invalid flag: %v", f))
		}

		ln.flags |= f
	}

	channel = make(chan Event, channelCap)
	ln.ch = channel
	
	if e.channels == nil { e.channels = map[string]chan Event{} }
	if e.listeners == nil { e.listeners = map[chan Event]*listener{} }

	patternStr := ln.patternID + ":" + pattern + "\n"

	if e.patterns == "" { e.patterns = "\n" }

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
func (e *Emitter) Once (pattern string, flags ...interface{}) (channel chan Event) {
	return e.On(pattern, append([]interface{}{Count,1}, flags...)...)
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
				e.patterns = strings.Replace(e.patterns, "\n" + l.patternID + ":" + l.pattern + "\n", "\n", 1)
				delete(e.listeners, ch)
				delete(e.channels, l.patternID)
				safeClose(l.ch)
			}
		}

		e.mu.Unlock()

		return 
	}

	if strings.Index(pattern,"\n") >= 0 { panic(fmt.Errorf("Incorrect pattern: %s", pattern)) }

	patternRes := strings.Replace(regexp.QuoteMeta(pattern), "\\*", ".+?", -1)
	patternRe,err := regexp.Compile("(?m)^(\\d+):" + patternRes + "\n")

	if err != nil { panic(fmt.Errorf("Incorrect pattern %s: %s", pattern, err)) }

	ids := patternRe.FindAllStringSubmatch(e.patterns, -1)

	for _,id := range ids {
		ch := e.channels[id[1]]
		l := e.listeners[ch]
		e.patterns = strings.Replace(e.patterns, "\n"+id[0], "\n", 1)
		
		delete(e.listeners, ch)
		delete(e.channels, id[1])
		safeClose(l.ch)
	}

	e.mu.Unlock()
}

// Emits event
func (e *Emitter) Emit (topic string, flagsArgs ...interface{}) (statusCh chan EmitStatus) {
	var event = &Event{
		Emitter: 	 e,
		Topic:       topic,
		status:      &EmitStatus{},
		statusUpdCh: make(chan bool),
		cancelCh:    make(chan bool),
		stickyCount: -1,
		used:        &sync.WaitGroup{},
	}

	i := 0
	l := len(flagsArgs)

	for i<l {
		f,ok := flagsArgs[i].(Flag)

		if !ok { break }

		i++

		switch f {
			case AtomicStatus, Sticky:
			case Count:
				f |= Sticky
				event.stickyCount = int64(flagsArgs[i].(int))
				i++
			default:
				panic(fmt.Errorf("Invalid flag: %v", f))
		}

		event.emitFlags |= f
	}

	if i!=0 { flagsArgs = flagsArgs[i:] }

	event.Args = flagsArgs

	isSticky := event.emitFlags & Sticky == Sticky

	e.mu.Lock()

	if isSticky { e.stickyEvents = append(e.stickyEvents, event) }

	statusCh = make(chan EmitStatus)

	listeners := e.getListeners(event.Topic)

	e.mu.Unlock()

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

	go e.emitEvent(event, listeners)

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
	topicRe := e.getTopicRe(topic)

	ids := topicRe.FindAllStringSubmatch(e.patterns,-1)

	for _,id := range ids {
		ch := e.channels[id[1]]

		listeners = append(listeners, e.listeners[ch])
	}

	return
}

func isOpened (ch chan Event) (is bool) {
	defer func() { recover() }()

	select {
		case ch<- Event{}:
		default:
	}

	return true
}

func (e *Emitter) emitEvent (rootEvent *Event, listeners []*listener) {
	defer func() { recover() }()

	rootEvent.used.Add(1)
	defer rootEvent.used.Add(-1)

	event := *rootEvent

	for _,l := range listeners {
		for _,fn := range l.middlewars {
			if isOpened(l.ch) {
				fn(&event)
			} else {
				defer e.Off("*", l.ch)
			}
		}

		isVoid := event.flags & Void == Void

		if isVoid { return }
	}

	isSticky := rootEvent.emitFlags & Sticky == Sticky
	isAtomicStatus := rootEvent.emitFlags & AtomicStatus == AtomicStatus

	if !isSticky { defer close(rootEvent.cancelCh) }

	if !isAtomicStatus {
		defer func() { rootEvent.statusUpdCh<- true }()
		
		event.statusUpdCh = make(chan bool, len(listeners))
		defer close(event.statusUpdCh)
	}

	sending := &sync.WaitGroup{}

	e.mu.Lock()

	for i := range listeners {
		l := listeners[i]

		isMiddleware := l.flags& Middleware == Middleware

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

		isHoldStatus := l.flags& HoldStatus == HoldStatus

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
			rootEvent.statusUpdCh<- true
		}()
	}

	e.mu.Unlock()
	
	sending.Wait()
}

func (e *Emitter) sendEvent (event Event, l *listener) (sent bool) {
	defer func () { recover() }()
	
	defer func() {
		err := recover()
		if err != nil { e.Off("*", l.ch) }

		event.status.Lock()
		event.status.Pending--
		if sent { event.status.Sent++ } else { event.status.Skipped++ }
		event.status.Unlock()

		event.statusUpdCh<- true
	}()

	isWait := l.flags& (Skip|Close) == 0

	if isWait {
		select {
			case <-event.cancelCh:
				return
			case l.ch<- event:
				sent=true
		}
	} else {
		select {
			case <-event.cancelCh:
				return
			case l.ch<- event:
				sent=true
			default:
				isClose := l.flags & Close == Close
				
				if isClose { e.Off("*", l.ch) }
		}
	}

	return 
}