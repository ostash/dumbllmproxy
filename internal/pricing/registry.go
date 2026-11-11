// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package pricing

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"dumbllmproxy/internal/usage"
)

// Facts are the immutable capture facts a calculator may use. They are read
// from the database at read time, so a calculator never depends on values
// stored at capture time.
type Facts struct {
	Model      string
	Usage      usage.Usage
	CapturedAt time.Time
}

// Result is a generic cost result. Details and Snapshot explain how the amount
// was obtained and are recalculated on every read.
type Result struct {
	Amount   float64
	Source   string
	Details  map[string]any
	Snapshot map[string]any
}

// Calculator computes a cost from capture facts. Implementations are
// provider-specific and are selected by the configured pricing name.
type Calculator interface {
	Calculate(Facts) (Result, error)
}

// Registry maps configured pricing names to calculators. The generic proxy
// never references a concrete calculator.
type Registry struct {
	calculators map[string]Calculator
}

func NewRegistry() *Registry {
	return &Registry{calculators: map[string]Calculator{}}
}

// Register adds a calculator under a pricing name. Names are matched
// case-insensitively, like the configuration values.
func (r *Registry) Register(name string, c Calculator) error {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return fmt.Errorf("pricing name must not be empty")
	}
	if c == nil {
		return fmt.Errorf("pricing calculator %q must not be nil", name)
	}
	if _, exists := r.calculators[name]; exists {
		return fmt.Errorf("pricing calculator %q is already registered", name)
	}
	r.calculators[name] = c
	return nil
}

// Calculate resolves a pricing name and evaluates it against the facts.
func (r *Registry) Calculate(name string, facts Facts) (Result, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || name == "none" {
		return Result{}, fmt.Errorf("no calculated pricing configured")
	}
	c, ok := r.calculators[name]
	if !ok {
		return Result{}, fmt.Errorf("no pricing calculator registered for %q", name)
	}
	return c.Calculate(facts)
}

// Names returns the registered pricing names in sorted order.
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.calculators))
	for name := range r.calculators {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
