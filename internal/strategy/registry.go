package strategy

import (
	"sort"
	"sync"
)

type Registry struct {
	mu         sync.RWMutex
	strategies map[string]Strategy
}

func NewRegistry() *Registry {
	return &Registry{
		strategies: make(map[string]Strategy),
	}
}

func (r *Registry) Register(s Strategy) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.strategies[s.Name()] = s
}

func (r *Registry) Get(name string) (Strategy, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.strategies[name]
	return s, ok
}

func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var names []string
	for _, s := range r.strategies {
		names = append(names, s.Name())
	}
	sort.Strings(names)
	return names
}

func (r *Registry) All() []Strategy {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []Strategy
	for _, s := range r.strategies {
		result = append(result, s)
	}
	return result
}

func DefaultRegistry() *Registry {
	r := NewRegistry()
	r.Register(NewMACrossover(5, 20))
	r.Register(NewMACD(12, 26, 9))
	r.Register(NewRSI(14, 30, 70))
	r.Register(NewBollinger(20, 2.0))
	r.Register(NewVolumeBreakout(20, 1.5))
	r.Register(NewValueMA60(60, 20, 2.0))
	r.Register(NewETFRotation(20, 5, 10, 2.0))
	r.Register(NewDividendDeviation(600, 0.8, 1.2))
	r.Register(NewBullFlag(10, 20, 0.5))
	r.Register(NewKDJ(9, 20, 80))
	r.Register(NewWilliamsR(14, -80, -20))
	r.Register(NewDonchian(20))
	r.Register(NewMFI(14, 20, 80))
	r.Register(NewParabolicSAR(0.02, 0.2))
	r.Register(NewROC(12, 5, -5))
	r.Register(NewMASticky(2.0, 1.5))
	r.Register(NewLimitUp(9.5, 1.2))
	r.Register(NewBottomReversal(20, -15, 1.5, 100, 0.5))
	r.Register(NewRelativeStrength(20, 60, 120, 10))
	r.Register(NewATRBreakout(20, 14, 20, 6, 1.2))
	r.Register(NewTrendPullback(2.5, 1.3))
	return r
}

func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.strategies)
}
