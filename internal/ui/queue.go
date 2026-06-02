package ui

import "github.com/liam-od/trawl/internal/fs"

type transferStatus int

const (
	statusPending transferStatus = iota
	statusActive
	statusDone
	statusFailed
)

// queueItem is one transfer in the queue. written/total are updated live from
// the active transfer's progress messages.
type queueItem struct {
	id      int
	name    string
	srcFS   fs.FS
	srcPath string
	dstFS   fs.FS
	dstPath string
	dstPane int // destination pane id; also implies copy direction
	status  transferStatus
	written int64
	total   int64
	err     error
}

// transferQueue holds queued, active, and finished transfers. Exactly one item
// is active at a time; the model starts the next pending item when one finishes.
type transferQueue struct {
	items   []*queueItem
	nextID  int
	visible bool
}

func (q *transferQueue) enqueue(it *queueItem) {
	it.id = q.nextID
	it.status = statusPending
	q.nextID++
	q.items = append(q.items, it)
}

// active returns the item currently transferring, or nil if none is.
func (q *transferQueue) active() *queueItem {
	for _, it := range q.items {
		if it.status == statusActive {
			return it
		}
	}
	return nil
}

// nextPending returns the earliest queued item not yet started, or nil.
func (q *transferQueue) nextPending() *queueItem {
	for _, it := range q.items {
		if it.status == statusPending {
			return it
		}
	}
	return nil
}
