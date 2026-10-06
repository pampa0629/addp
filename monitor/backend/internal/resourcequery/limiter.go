package resourcequery

import "sync"

// Counts are independent of the current budget version: reducing limits admits
// no new requests until existing work drains. There is no waiting queue.
type Limiter struct {
	mu       sync.Mutex
	total    int
	subjects map[string]int
}

func (l *Limiter) Acquire(subject string, b Budget) (func(), error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if subject == "" || b.Validate() != nil {
		return nil, ErrInvalid
	}
	if l.total >= b.ProcessConcurrency || l.subjects[subject] >= b.SubjectConcurrency {
		return nil, ErrBusy
	}
	if l.subjects == nil {
		l.subjects = map[string]int{}
	}
	l.total++
	l.subjects[subject]++
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			l.total--
			l.subjects[subject]--
			if l.subjects[subject] == 0 {
				delete(l.subjects, subject)
			}
		})
	}, nil
}

// Check applies a committed lower limit to the active admission count. The
// caller already holds a hard-cap lease; rejected work releases that same lease.
func (l *Limiter) Check(subject string, b Budget) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if b.Validate() != nil || l.subjects[subject] < 1 {
		return ErrInvalid
	}
	if l.total > b.ProcessConcurrency || l.subjects[subject] > b.SubjectConcurrency {
		return ErrBusy
	}
	return nil
}
