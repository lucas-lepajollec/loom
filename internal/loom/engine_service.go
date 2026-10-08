package loom

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"sync"
	"time"
)

var errEngineBusy = errors.New("model_busy: engine reserved for another model")

type engineServicePolicy struct {
	Idle  int `json:"idle_unload_minutes"`
	Wait  int `json:"swap_wait_seconds"`
	Grace int `json:"interactive_grace_minutes"`
	Max   int `json:"models_max"`
}

func readEngineServicePolicy() engineServicePolicy {
	p := engineServicePolicy{Idle: 30, Wait: 300, Grace: 15, Max: routerModelsMax()}
	cfg := ReadConfig()
	for key, dst := range map[string]*int{"ENGINE_IDLE_UNLOAD": &p.Idle, "ENGINE_SWAP_WAIT": &p.Wait, "ENGINE_INTERACTIVE_GRACE": &p.Grace} {
		if n, err := strconv.Atoi(cfg[key]); err == nil && n >= 0 {
			*dst = n
		}
	}
	return p
}

type engineResident struct {
	Model       string    `json:"model"`
	InFlight    int       `json:"in_flight"`
	Since       time.Time `json:"since"`
	LastUsed    time.Time `json:"last_used"`
	interactive time.Time
	started     []time.Time
}
type engineWaiter struct {
	ctx         context.Context
	model       string
	priority    string
	since       time.Time
	started     time.Time
	load        func() error
	done        chan struct{}
	err         error
	granted     bool
	maintenance bool
}

// La file ne pilote ni les slots ni les tokens : elle protège seulement les
// changements de résidence. Une réservation dure jusqu'à la fin du stream.
type engineService struct {
	mu              sync.Mutex
	now             func() time.Time
	policy          func() engineServicePolicy
	capacity        func() int
	observe         func() ([]string, error)
	unload          func() error
	nativeBusy      func() bool
	resident        map[string]*engineResident
	queue           []*engineWaiter
	loading         bool
	lastActivity    time.Time
	lastInteractive time.Time
}

func newEngineService(now func() time.Time, policy func() engineServicePolicy, observe func() ([]string, error), unload func() error) *engineService {
	return &engineService{now: now, policy: policy, observe: observe, unload: unload, resident: map[string]*engineResident{}, lastActivity: now()}
}

func (s *engineService) refreshLocked() error {
	models, err := s.observe()
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, model := range models {
		seen[model] = true
		if s.resident[model] == nil {
			s.resident[model] = &engineResident{Model: model, LastUsed: s.lastActivity}
		}
	}
	for model, r := range s.resident {
		if !seen[model] && r.InFlight == 0 {
			delete(s.resident, model)
		}
	}
	return nil
}

func (s *engineService) backgroundOK(model string, p engineServicePolicy) bool {
	if s.resident[model] != nil {
		return true
	}
	// Un travail de fond absent ne charge que lorsque toute la VRAM est libre.
	return len(s.resident) == 0 && (s.lastInteractive.IsZero() || s.now().Sub(s.lastInteractive) >= time.Duration(p.Grace)*time.Minute)
}

func (s *engineService) reserveLocked(w *engineWaiter) {
	now := s.now()
	r := s.resident[w.model]
	if r == nil {
		r = &engineResident{Model: w.model}
		s.resident[w.model] = r
	}
	r.started = append(r.started, now)
	r.InFlight = len(r.started)
	r.Since = r.started[0]
	r.LastUsed, s.lastActivity = now, now
	if w.priority == "interactive" {
		r.interactive, s.lastInteractive = now, now
	}
	w.granted = true
	w.started = now
	close(w.done)
}

func (s *engineService) releaseLocked(model string, started time.Time, priority string) {
	if r := s.resident[model]; r != nil {
		for i, at := range r.started {
			if at.Equal(started) {
				r.started = append(r.started[:i], r.started[i+1:]...)
				break
			}
		}
		r.InFlight = len(r.started)
		r.Since = time.Time{}
		if r.InFlight > 0 {
			r.Since = r.started[0]
		}
		r.LastUsed = s.now()
		if priority == "interactive" {
			s.lastInteractive = s.now()
		}
	}
	s.lastActivity = s.now()
}

// Les arrivées pour la cible de tête rejoignent sa salve, même si une autre
// cible attend déjà derrière. Les anciens résidents ne passent pas devant.
func (s *engineService) dispatchLocked() {
	if s.loading || len(s.queue) == 0 {
		return
	}
	for len(s.queue) > 0 && s.queue[0].ctx.Err() != nil {
		w := s.queue[0]
		s.queue = s.queue[1:]
		w.err = errEngineBusy
		close(w.done)
	}
	if len(s.queue) == 0 {
		return
	}
	w := s.queue[0]
	if !w.maintenance && s.resident[w.model] != nil {
		next := s.queue[:0]
		for _, q := range s.queue {
			if !q.maintenance && q.model == w.model {
				s.reserveLocked(q)
			} else {
				next = append(next, q)
			}
		}
		s.queue = next
		s.dispatchLocked()
		return
	}
	p := s.policy()
	if w.priority == "background" && !s.backgroundOK(w.model, p) {
		s.queue = s.queue[1:]
		w.err = errEngineBusy
		close(w.done)
		s.dispatchLocked()
		return
	}
	// En multirésident, seul un chargement pouvant évincer attend le drain.
	max := p.Max
	if s.capacity != nil {
		max = s.capacity()
	}
	if w.maintenance || max > 0 && len(s.resident) >= max {
		for _, r := range s.resident {
			if r.InFlight > 0 {
				return
			}
		}
	}
	s.loading = true
	go func() {
		err := w.load()
		s.mu.Lock()
		defer s.mu.Unlock()
		s.loading = false
		if err == nil {
			err = s.refreshLocked()
		}
		if w.maintenance {
			for i, q := range s.queue {
				if q == w {
					s.queue = append(s.queue[:i], s.queue[i+1:]...)
					w.err = err
					close(w.done)
					break
				}
			}
			s.dispatchLocked()
			return
		}
		if err == nil && s.resident[w.model] == nil {
			s.resident[w.model] = &engineResident{Model: w.model}
		}
		if err != nil {
			next := s.queue[:0]
			for _, q := range s.queue {
				if !q.maintenance && q.model == w.model {
					q.err = err
					close(q.done)
				} else {
					next = append(next, q)
				}
			}
			s.queue = next
		}
		s.dispatchLocked()
	}()
}

// Les changements explicites de paramètres du moteur utilisent la même
// barrière FIFO, sans compter une opération de contrôle comme une inférence.
func (s *engineService) maintain(ctx context.Context, model string, action func() error) error {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(s.policy().Wait)*time.Second)
	defer cancel()
	w := &engineWaiter{ctx: ctx, model: model, priority: "interactive", since: s.now(), load: action, done: make(chan struct{}), maintenance: true}
	s.mu.Lock()
	if !s.loading {
		if err := s.refreshLocked(); err != nil {
			s.mu.Unlock()
			return err
		}
	}
	s.queue = append(s.queue, w)
	s.dispatchLocked()
	s.mu.Unlock()
	select {
	case <-w.done:
	case <-ctx.Done():
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		for i, q := range s.queue {
			if q == w {
				s.queue = append(s.queue[:i], s.queue[i+1:]...)
				break
			}
		}
		s.dispatchLocked()
		return errEngineBusy
	}
	return w.err
}

func (s *engineService) acquire(ctx context.Context, model, priority string, load func() error) (func(), error) {
	p := s.policy()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(p.Wait)*time.Second)
	defer cancel()
	w := &engineWaiter{ctx: ctx, model: model, priority: priority, since: s.now(), load: load, done: make(chan struct{})}
	s.mu.Lock()
	if ctx.Err() != nil {
		s.mu.Unlock()
		return nil, errEngineBusy
	}
	if !s.loading {
		if err := s.refreshLocked(); err != nil {
			s.mu.Unlock()
			return nil, err
		}
	}
	if priority == "background" && (!s.backgroundOK(model, p) || s.loading && s.resident[model] == nil) {
		s.mu.Unlock()
		return nil, errEngineBusy
	}
	if !s.loading && len(s.queue) == 0 && s.resident[model] != nil {
		s.reserveLocked(w)
	} else {
		s.queue = append(s.queue, w)
		s.dispatchLocked()
	}
	s.mu.Unlock()
	select {
	case <-w.done:
	case <-ctx.Done():
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		if w.granted {
			s.releaseLocked(model, w.started, priority)
		} else {
			for i, q := range s.queue {
				if q == w {
					s.queue = append(s.queue[:i], s.queue[i+1:]...)
					break
				}
			}
		}
		s.dispatchLocked()
		return nil, errEngineBusy
	}
	if w.err != nil {
		return nil, w.err
	}
	// Le début de réservation est distinct de l'arrivée dans la file.
	started := w.started
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.releaseLocked(model, started, priority)
			s.dispatchLocked()
		})
	}, nil
}

func (s *engineService) idleTick() {
	s.mu.Lock()
	p := s.policy()
	if p.Idle == 0 || s.loading || len(s.queue) > 0 || s.now().Sub(s.lastActivity) < time.Duration(p.Idle)*time.Minute {
		s.mu.Unlock()
		return
	}
	if s.nativeBusy != nil && s.nativeBusy() {
		s.mu.Unlock()
		return
	}
	if s.refreshLocked() != nil || len(s.resident) == 0 {
		s.mu.Unlock()
		return
	}
	for _, r := range s.resident {
		if r.InFlight > 0 {
			s.mu.Unlock()
			return
		}
	}
	s.loading = true
	s.mu.Unlock()
	err := s.unload()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loading = false
	if err == nil {
		s.resident = map[string]*engineResident{}
	}
	s.dispatchLocked()
}

func (s *engineService) snapshot() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.loading {
		_ = s.refreshLocked()
	}
	p := s.policy()
	resident := []engineResident{}
	for _, r := range s.resident {
		resident = append(resident, *r)
	}
	sort.Slice(resident, func(i, j int) bool { return resident[i].Model < resident[j].Model })
	type batch struct {
		Model   string    `json:"model"`
		Waiting int       `json:"waiting"`
		Since   time.Time `json:"since"`
	}
	queue := []batch{}
	index := map[string]int{}
	for _, w := range s.queue {
		i, ok := index[w.model]
		if !ok {
			i = len(queue)
			index[w.model] = i
			queue = append(queue, batch{Model: w.model, Since: w.since})
		}
		queue[i].Waiting++
	}
	var unloadAt *time.Time
	if p.Idle > 0 && len(resident) > 0 {
		at := s.lastActivity.Add(time.Duration(p.Idle) * time.Minute)
		unloadAt = &at
	}
	return map[string]any{"idle_unload_minutes": p.Idle, "swap_wait_seconds": p.Wait, "interactive_grace_minutes": p.Grace, "models_max": p.Max, "resident": resident, "queue": queue, "last_activity": s.lastActivity, "idle_unload_at": unloadAt}
}

var engineServiceOwner struct {
	sync.Mutex
	home    string
	service *engineService
}

func currentEngineService() *engineService {
	engineServiceOwner.Lock()
	defer engineServiceOwner.Unlock()
	if engineServiceOwner.service == nil || engineServiceOwner.home != LoomHome() {
		engineServiceOwner.home = LoomHome()
		engineServiceOwner.service = newEngineService(func() time.Time { return time.Now().UTC() }, readEngineServicePolicy, serviceResidentModels, serviceIdleUnload)
		engineServiceOwner.service.capacity = serviceCapacity
		engineServiceOwner.service.nativeBusy = engineNativeGenerating
	}
	return engineServiceOwner.service
}
