// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package pricing

import (
	"fmt"
	"sort"
	"strings"
	"time"
	_ "time/tzdata"

	"dumbllmproxy/internal/config"
)

type DeepSeek struct {
	cfg        config.DeepSeekPricing
	scheduleTZ *time.Location
	holidayTZ  *time.Location
	holidays   map[string]bool
	models     map[string][]compiledRate
}

type compiledWindow struct {
	start time.Duration
	end   time.Duration
}

type compiledConditions struct {
	from         *time.Time
	to           *time.Time
	fromLabel    string
	toLabel      string
	days         map[time.Weekday]bool
	windows      []compiledWindow
	holiday      *bool
	dates        map[string]bool
	excludeDates map[string]bool
}

type compiledRate struct {
	name       string
	card       config.RateCard
	conditions compiledConditions
}

func NewDeepSeek(cfg config.DeepSeekPricing) (*DeepSeek, error) {
	scheduleTZ, err := time.LoadLocation(cfg.ScheduleTimezone)
	if err != nil {
		return nil, fmt.Errorf("pricing.deepseek.schedule_timezone: %w", err)
	}
	holidayTZ, err := time.LoadLocation(cfg.HolidayTimezone)
	if err != nil {
		return nil, fmt.Errorf("pricing.deepseek.holiday_timezone: %w", err)
	}
	d := &DeepSeek{
		cfg:        cfg,
		scheduleTZ: scheduleTZ,
		holidayTZ:  holidayTZ,
		holidays:   make(map[string]bool, len(cfg.Holidays)),
	}
	for _, date := range cfg.Holidays {
		key, err := parseDateKey(date, holidayTZ)
		if err != nil {
			return nil, fmt.Errorf("pricing.deepseek.holidays: %w", err)
		}
		d.holidays[key] = true
	}
	if len(cfg.Models) == 0 {
		return nil, fmt.Errorf("pricing.deepseek.models must not be empty")
	}
	d.models = make(map[string][]compiledRate, len(cfg.Models))
	for rawModel, modelCfg := range cfg.Models {
		model := strings.TrimSpace(rawModel)
		if model == "" {
			return nil, fmt.Errorf("pricing.deepseek.models: model name must not be empty")
		}
		if _, exists := d.models[model]; exists {
			return nil, fmt.Errorf("pricing.deepseek.models: duplicate model %q", model)
		}
		if len(modelCfg.Rates) == 0 {
			return nil, fmt.Errorf("pricing.deepseek.models.%s.rates must not be empty", model)
		}

		rates := make([]compiledRate, 0, len(modelCfg.Rates))
		for i, r := range modelCfg.Rates {
			conditions, err := compileConditions(r.Conditions, scheduleTZ, holidayTZ)
			if err != nil {
				return nil, fmt.Errorf("pricing.deepseek.models.%s.rates[%d].conditions: %w", model, i, err)
			}
			rates = append(rates, compiledRate{
				name: strings.TrimSpace(r.Name),
				card: config.RateCard{
					InputCacheHit:  r.InputCacheHit,
					InputCacheMiss: r.InputCacheMiss,
					Output:         r.Output,
				},
				conditions: conditions,
			})
		}
		d.models[model] = rates
	}
	return d, nil
}

// Calculate computes a DeepSeek cost from the capture facts. It deliberately
// refuses to guess when a required field is missing.
func (d *DeepSeek) Calculate(facts Facts) (Result, error) {
	model := strings.TrimSpace(facts.Model)
	u := facts.Usage
	at := facts.CapturedAt
	rates, ok := d.models[model]
	if !ok {
		return Result{}, fmt.Errorf("no pricing configured for model %q", model)
	}
	if u.CacheHitTokens == nil {
		return Result{}, fmt.Errorf("missing prompt_cache_hit_tokens")
	}
	if u.CacheMissTokens == nil {
		return Result{}, fmt.Errorf("missing prompt_cache_miss_tokens")
	}
	if u.OutputTokens == nil {
		return Result{}, fmt.Errorf("missing completion_tokens")
	}

	local := at.In(d.scheduleTZ)
	holidayDate := at.In(d.holidayTZ).Format("2006-01-02")
	holiday := d.holidays[holidayDate]
	var rate compiledRate
	found := false
	for _, candidate := range rates {
		if candidate.conditions.matches(local, holiday, holidayDate) {
			rate = candidate
			found = true
			break
		}
	}
	if !found {
		return Result{}, fmt.Errorf("no pricing rate matched at %s", local.Format(time.RFC3339))
	}
	card := rate.card

	hit := *u.CacheHitTokens
	miss := *u.CacheMissTokens
	out := *u.OutputTokens
	divisor := 1.0
	switch strings.ToLower(d.cfg.Unit) {
	case "", "per_million_tokens", "per_million", "1m":
		divisor = 1_000_000
	case "per_token", "token":
		divisor = 1
	default:
		return Result{}, fmt.Errorf("unsupported pricing unit %q", d.cfg.Unit)
	}

	hitCost := float64(hit) * card.InputCacheHit / divisor
	missCost := float64(miss) * card.InputCacheMiss / divisor
	outCost := float64(out) * card.Output / divisor
	amount := hitCost + missCost + outCost

	details := map[string]any{
		"source":            "deepseek_calculated",
		"model":             model,
		"unit":              d.cfg.Unit,
		"schedule_timezone": d.cfg.ScheduleTimezone,
		"holiday_timezone":  d.cfg.HolidayTimezone,
		"local_time":        local.Format(time.RFC3339),
		"holiday_date":      holidayDate,
		"holiday":           holiday,
		"cache_hit_tokens":  hit,
		"cache_miss_tokens": miss,
		"output_tokens":     out,
		"cache_hit_rate":    card.InputCacheHit,
		"cache_miss_rate":   card.InputCacheMiss,
		"output_rate":       card.Output,
		"cache_hit_cost":    hitCost,
		"cache_miss_cost":   missCost,
		"output_cost":       outCost,
		"formula":           "(cache_hit_tokens*cache_hit_rate + cache_miss_tokens*cache_miss_rate + output_tokens*output_rate) / unit",
	}
	snapshot := map[string]any{
		"provider":          "deepseek",
		"model":             model,
		"unit":              d.cfg.Unit,
		"schedule_timezone": d.cfg.ScheduleTimezone,
		"holiday_timezone":  d.cfg.HolidayTimezone,
		"rates": map[string]any{
			"input_cache_hit":  card.InputCacheHit,
			"input_cache_miss": card.InputCacheMiss,
			"output":           card.Output,
		},
	}
	if rate.name != "" {
		details["rate_name"] = rate.name
		snapshot["rate_name"] = rate.name
	}
	if rate.conditions.fromLabel != "" {
		details["rate_from"] = rate.conditions.fromLabel
		snapshot["rate_from"] = rate.conditions.fromLabel
	}
	if rate.conditions.toLabel != "" {
		details["rate_to"] = rate.conditions.toLabel
		snapshot["rate_to"] = rate.conditions.toLabel
	}
	return Result{
		Amount:   amount,
		Source:   "deepseek_calculated",
		Details:  details,
		Snapshot: snapshot,
	}, nil
}

func compileConditions(cfg *config.RateConditions, scheduleTZ, holidayTZ *time.Location) (compiledConditions, error) {
	var c compiledConditions
	if cfg == nil {
		return c, nil
	}
	from, fromLabel, err := parseBound(cfg.From, scheduleTZ, false)
	if err != nil {
		return compiledConditions{}, fmt.Errorf("from: %w", err)
	}
	to, toLabel, err := parseBound(cfg.To, scheduleTZ, true)
	if err != nil {
		return compiledConditions{}, fmt.Errorf("to: %w", err)
	}
	if from != nil && to != nil && to.Before(*from) {
		return compiledConditions{}, fmt.Errorf("to must not be before from (%s < %s)", cfg.To, cfg.From)
	}
	c.from = from
	c.to = to
	c.fromLabel = fromLabel
	c.toLabel = toLabel

	if len(cfg.Days) > 0 {
		c.days = make(map[time.Weekday]bool, len(cfg.Days))
		for _, name := range cfg.Days {
			day, ok := parseWeekday(name)
			if !ok {
				return compiledConditions{}, fmt.Errorf("invalid day %q", name)
			}
			c.days[day] = true
		}
	}
	for i, w := range cfg.TimeWindows {
		start, err := parseClock(w.Start)
		if err != nil {
			return compiledConditions{}, fmt.Errorf("time_windows[%d]: invalid start %q", i, w.Start)
		}
		end, err := parseClock(w.End)
		if err != nil {
			return compiledConditions{}, fmt.Errorf("time_windows[%d]: invalid end %q", i, w.End)
		}
		if end <= start {
			return compiledConditions{}, fmt.Errorf("time_windows[%d]: end must be after start (%s-%s)", i, w.Start, w.End)
		}
		c.windows = append(c.windows, compiledWindow{start: start, end: end})
	}
	if cfg.Holiday != nil {
		h := *cfg.Holiday
		c.holiday = &h
	}
	if len(cfg.Dates) > 0 {
		c.dates = make(map[string]bool, len(cfg.Dates))
		for _, date := range cfg.Dates {
			key, err := parseDateKey(date, holidayTZ)
			if err != nil {
				return compiledConditions{}, fmt.Errorf("dates: %w", err)
			}
			c.dates[key] = true
		}
	}
	if len(cfg.ExcludeDates) > 0 {
		c.excludeDates = make(map[string]bool, len(cfg.ExcludeDates))
		for _, date := range cfg.ExcludeDates {
			key, err := parseDateKey(date, holidayTZ)
			if err != nil {
				return compiledConditions{}, fmt.Errorf("exclude_dates: %w", err)
			}
			c.excludeDates[key] = true
		}
	}
	return c, nil
}

func (c compiledConditions) matches(local time.Time, holiday bool, holidayDate string) bool {
	if c.from != nil && local.Before(*c.from) {
		return false
	}
	if c.to != nil && local.After(*c.to) {
		return false
	}
	if len(c.days) > 0 && !c.days[local.Weekday()] {
		return false
	}
	if len(c.windows) > 0 {
		seconds := time.Duration(local.Hour())*time.Hour +
			time.Duration(local.Minute())*time.Minute +
			time.Duration(local.Second())*time.Second +
			time.Duration(local.Nanosecond())
		matched := false
		for _, w := range c.windows {
			if seconds >= w.start && seconds < w.end {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if c.holiday != nil && holiday != *c.holiday {
		return false
	}
	if len(c.dates) > 0 && !c.dates[holidayDate] {
		return false
	}
	if c.excludeDates[holidayDate] {
		return false
	}
	return true
}

func parseBound(s string, loc *time.Location, endOfDay bool) (*time.Time, string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, "", nil
	}
	if t, err := time.ParseInLocation("2006-01-02", s, loc); err == nil {
		if endOfDay {
			t = t.AddDate(0, 0, 1).Add(-time.Nanosecond)
		}
		return &t, s, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		t = t.In(loc)
		return &t, s, nil
	}
	return nil, "", fmt.Errorf("invalid date %q", s)
}

func parseDateKey(s string, loc *time.Location) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("empty date")
	}
	if t, err := time.ParseInLocation("2006-01-02", s, loc); err == nil {
		return t.Format("2006-01-02"), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.In(loc).Format("2006-01-02"), nil
	}
	return "", fmt.Errorf("invalid date %q", s)
}

func parseClock(s string) (time.Duration, error) {
	t, err := time.Parse("15:04", strings.TrimSpace(s))
	if err != nil {
		return 0, err
	}
	return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute, nil
}

func parseWeekday(s string) (time.Weekday, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "sun", "sunday":
		return time.Sunday, true
	case "mon", "monday":
		return time.Monday, true
	case "tue", "tues", "tuesday":
		return time.Tuesday, true
	case "wed", "wednesday":
		return time.Wednesday, true
	case "thu", "thur", "thurs", "thursday":
		return time.Thursday, true
	case "fri", "friday":
		return time.Friday, true
	case "sat", "saturday":
		return time.Saturday, true
	default:
		return 0, false
	}
}

// Diagnostics reports likely mistakes in the configured schedule. It is
// advisory: the calculator still evaluates rates in configuration order.
func (d *DeepSeek) Diagnostics() []string {
	models := make([]string, 0, len(d.models))
	for model := range d.models {
		models = append(models, model)
	}
	sort.Strings(models)

	var out []string
	for _, model := range models {
		out = append(out, diagnoseModel(model, d.models[model])...)
	}
	return out
}

func diagnoseModel(model string, rates []compiledRate) []string {
	var out []string
	catchAll := -1
	hasDefault := false
	for i, rate := range rates {
		if rate.conditions.isCatchAll() {
			catchAll = i
			break
		}
		// A date-only rate without an upper bound is a practical default: it
		// keeps applying to current and future requests.
		if rate.conditions.dateOnly() && rate.conditions.to == nil {
			hasDefault = true
		}
	}
	if catchAll >= 0 && catchAll < len(rates)-1 {
		out = append(out, fmt.Sprintf(
			"pricing.deepseek.models.%s: rate %q has no conditions and shadows %d later rate(s)",
			model, rateLabel(rates[catchAll], catchAll), len(rates)-catchAll-1))
	}
	if catchAll < 0 && !hasDefault {
		out = append(out, fmt.Sprintf(
			"pricing.deepseek.models.%s: no catch-all rate; requests outside the configured conditions will have unavailable cost",
			model))
	}

	// Report date coverage gaps that are visible from the configured bounds.
	var earliest, latest *time.Time
	unboundedEnd := false
	for _, rate := range rates {
		c := rate.conditions
		if c.from != nil && (earliest == nil || c.from.Before(*earliest)) {
			earliest = c.from
		}
		if c.to == nil {
			unboundedEnd = true
		} else if latest == nil || c.to.After(*latest) {
			latest = c.to
		}
	}
	if earliest != nil {
		out = append(out, fmt.Sprintf(
			"pricing.deepseek.models.%s: no rate applies before %s; earlier requests will have unavailable cost",
			model, earliest.Format("2006-01-02")))
	}
	if !unboundedEnd && latest != nil {
		out = append(out, fmt.Sprintf(
			"pricing.deepseek.models.%s: no rate applies after %s; later requests will have unavailable cost",
			model, latest.Format("2006-01-02")))
	}

	// Report obvious shadowing by an earlier date-only rule.
	for i := 0; i < len(rates); i++ {
		for j := i + 1; j < len(rates); j++ {
			if rates[i].conditions.isCatchAll() {
				continue // already reported above
			}
			if rates[i].conditions.coversDateRange(rates[j].conditions) {
				out = append(out, fmt.Sprintf(
					"pricing.deepseek.models.%s: rate %q shadows later rate %q",
					model, rateLabel(rates[i], i), rateLabel(rates[j], j)))
			}
		}
	}
	return out
}

func rateLabel(rate compiledRate, index int) string {
	if rate.name != "" {
		return rate.name
	}
	return fmt.Sprintf("#%d", index+1)
}

func (c compiledConditions) isCatchAll() bool {
	return c.from == nil && c.to == nil && len(c.days) == 0 && len(c.windows) == 0 &&
		c.holiday == nil && len(c.dates) == 0 && len(c.excludeDates) == 0
}

func (c compiledConditions) dateOnly() bool {
	return len(c.days) == 0 && len(c.windows) == 0 && c.holiday == nil &&
		len(c.dates) == 0 && len(c.excludeDates) == 0
}

// coversDateRange reports whether c matches every date that o matches, for
// date-only conditions. It is a conservative check used for diagnostics only.
func (c compiledConditions) coversDateRange(o compiledConditions) bool {
	if !c.dateOnly() || !o.dateOnly() {
		return false
	}
	if c.from != nil && (o.from == nil || o.from.Before(*c.from)) {
		return false
	}
	if c.to != nil && (o.to == nil || o.to.After(*c.to)) {
		return false
	}
	return true
}
