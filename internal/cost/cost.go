// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

// Package cost bridges captured request facts to the configured pricing
// calculators. It is used at read time, so calculated costs always reflect the
// active configuration.
package cost

import (
	"encoding/json"

	"dumbllmproxy/internal/pricing"
	"dumbllmproxy/internal/store"
)

// Resolver resolves request costs using a pricing registry.
type Resolver struct {
	registry *pricing.Registry
}

func NewResolver(registry *pricing.Registry) *Resolver {
	if registry == nil {
		registry = pricing.NewRegistry()
	}
	return &Resolver{registry: registry}
}

// Resolve implements store.CostResolver.
func (r *Resolver) Resolve(facts store.CostFacts) store.Cost {
	// Provider-reported cost is a captured fact and takes precedence.
	if facts.ProviderCost != nil {
		amount := *facts.ProviderCost
		return store.Cost{
			Amount: &amount,
			Source: "openrouter_reported",
			DetailsJSON: marshal(map[string]any{
				"source": "openrouter_reported",
				"field":  "usage.cost",
				"amount": amount,
			}),
		}
	}

	result, err := r.registry.Calculate(facts.Pricing, pricing.Facts{
		Model:      facts.Model,
		Usage:      facts.Usage,
		CapturedAt: facts.CapturedAt,
	})
	if err != nil {
		return store.Cost{
			DetailsJSON: marshal(map[string]any{
				"available": false,
				"reason":    err.Error(),
			}),
		}
	}
	amount := result.Amount
	return store.Cost{
		Amount:       &amount,
		Source:       result.Source,
		DetailsJSON:  marshal(result.Details),
		SnapshotJSON: marshal(result.Snapshot),
	}
}

func marshal(v any) string {
	if v == nil {
		return ""
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(raw)
}
