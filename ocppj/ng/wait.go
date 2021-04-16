package ocppj

import ws "github.com/lorenzodonini/ocpp-go/ws/ng"

// ReadOrDrop holds a channel along with a channel to signal being done.
type ReadOrDrop struct {
	Read <-chan RequestResultPair
	Drop chan<- struct{}
}

// WriteOrDropped holds a channel along with a signal whether it's done.
type WriteOrDropped struct {
	Write   chan<- RequestResultPair
	Dropped <-chan struct{}
}

type waitClose struct {
	ch    chan struct{}
	cnt   int
	owned ws.WriteOrDropped
}

type wait chan waitClose

func newWait(owned ws.WriteOrDropped) wait {
	w := make(chan waitClose, 1)
	w <- waitClose{
		ch:    make(chan struct{}),
		owned: owned,
	}

	return w
}

// Channel gives access to the send channel.
func (w wait) Channel() ws.WriteOrDropped {
	wc := <-w
	w <- wc

	return wc.owned
}

func (w wait) add(i int) {
	wc := <-w
	wc.cnt = wc.cnt + i
	if wc.cnt == 0 {
		close(wc.ch)
	}
	w <- wc
}

func (w wait) Go() {
	w.add(1)
}

func (w wait) Done() {
	w.add(-1)
}

// GoCloseWhenDone launches a go routine to wait on producers and close channel
// when all are done. It must only be called once!
func (w wait) GoCloseWhenDone() {
	go func() {
		wc := <-w
		ch := wc.ch
		w <- wc

		<-ch

		close(wc.owned.Write)
		close(w)
	}()
}
