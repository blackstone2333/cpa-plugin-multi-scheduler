package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

func ParseRFC3339(val string) (time.Time, error) {
	val = strings.TrimSpace(val)
	if val == "" {
		return time.Time{}, errors.New("reset time is missing")
	}
	t, err := time.Parse(time.RFC3339, val)
	if err == nil {
		return t.UTC(), nil
	}
	// Try without timezone if needed
	t, err = time.Parse("2006-01-02T15:04:05", val)
	if err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("invalid rfc3339 time: %s", val)
}

func ParseResetTime(val any, resetAfter any) (time.Time, error) {
	if s, ok := val.(string); ok && strings.TrimSpace(s) != "" {
		s = strings.TrimSpace(s)
		if num, err := strconv.ParseFloat(s, 64); err == nil && num > 0 {
			return ParseResetTime(num, nil)
		}
		return ParseRFC3339(s)
	}
	if num, ok := toFloat(val); ok && num > 0 {
		if num > 10000000000 {
			num /= 1000.0
		}
		sec := int64(num)
		nsec := int64((num - float64(sec)) * 1e9)
		return time.Unix(sec, nsec).UTC(), nil
	}
	if after, ok := toFloat(resetAfter); ok && after > 0 {
		return time.Now().UTC().Add(time.Duration(after * float64(time.Second))), nil
	}
	return time.Time{}, errors.New("reset time is missing or invalid")
}

func toFloat(val any) (float64, bool) {
	if val == nil {
		return 0, false
	}
	switch v := val.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case string:
		v = strings.TrimSpace(strings.TrimSuffix(v, "%"))
		f, err := strconv.ParseFloat(v, 64)
		return f, err == nil
	}
	return 0, false
}

func NormalizeFraction(val any) (float64, error) {
	num, ok := toFloat(val)
	if !ok {
		return 0, errors.New("invalid fraction")
	}
	if num > 1.0 && num <= 100.0 {
		num /= 100.0
	}
	if num < 0.0 || num > 1.0 {
		return 0, errors.New("fraction outside 0..1")
	}
	return num, nil
}

func RemainingFromUsedPercent(val any, exhausted bool) (float64, error) {
	if val == nil {
		if exhausted {
			return 0.0, nil
		}
		return 0.0, errors.New("utilization is missing")
	}
	used, ok := toFloat(val)
	if !ok {
		return 0.0, errors.New("utilization is invalid")
	}
	if used < 0.0 || used > 100.0 {
		return 0.0, errors.New("utilization outside 0..100")
	}
	return math.Max(0.0, (100.0-used)/100.0), nil
}

// ParseAntiGravityQuota parses the Gemini quota group from AntiGravity response.
func ParseAntiGravityQuota(raw []byte) (*Quota, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("json decode: %w", err)
	}

	groups, ok := payload["groups"].([]any)
	if !ok {
		return nil, errors.New("missing groups array")
	}

	var geminiGroup map[string]any
	for _, g := range groups {
		grp, ok := g.(map[string]any)
		if !ok {
			continue
		}
		desc := strings.ToLower(fmt.Sprintf("%v %v %v", grp["displayName"], grp["display_name"], grp["description"]))
		if strings.Contains(desc, "gemini") {
			geminiGroup = grp
			break
		}
	}
	if geminiGroup == nil {
		return nil, errors.New("expected Gemini quota group in AntiGravity summary")
	}

	buckets, ok := geminiGroup["buckets"].([]any)
	if !ok {
		return nil, errors.New("missing buckets in Gemini group")
	}

	var fiveVals []float64
	type weeklyEntry struct {
		frac  float64
		reset time.Time
	}
	var weeklyEntries []weeklyEntry

	for _, b := range buckets {
		bucket, ok := b.(map[string]any)
		if !ok {
			continue
		}
		window := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", bucket["window"])))
		rawFrac := bucket["remainingFraction"]
		if rawFrac == nil {
			rawFrac = bucket["remaining_fraction"]
		}
		frac, err := NormalizeFraction(rawFrac)
		if err != nil {
			continue
		}
		if window == "5h" || window == "five-hour" || window == "five_hour" {
			fiveVals = append(fiveVals, frac)
		} else if window == "weekly" || window == "week" {
			rawReset := bucket["resetTime"]
			if rawReset == nil {
				rawReset = bucket["reset_time"]
			}
			rt, err := ParseResetTime(rawReset, nil)
			if err == nil {
				weeklyEntries = append(weeklyEntries, weeklyEntry{frac: frac, reset: rt})
			}
		}
	}

	if len(weeklyEntries) == 0 {
		return nil, errors.New("weekly Gemini quota bucket missing")
	}
	sort.Slice(weeklyEntries, func(i, j int) bool {
		return weeklyEntries[i].reset.Before(weeklyEntries[j].reset)
	})

	var fivePtr *float64
	if len(fiveVals) > 0 {
		minFive := fiveVals[0]
		for _, v := range fiveVals[1:] {
			if v < minFive {
				minFive = v
			}
		}
		fivePtr = &minFive
	}

	longFracs := make([]float64, len(weeklyEntries))
	for i, w := range weeklyEntries {
		longFracs[i] = w.frac
	}

	return &Quota{
		FiveHourFraction: fivePtr,
		WeeklyFraction:   weeklyEntries[0].frac,
		WeeklyReset:      weeklyEntries[0].reset,
		WeeklyFractions:  longFracs,
		LastPolled:       time.Now().UTC(),
	}, nil
}

// ParseClaudeQuota parses Anthropic Claude OAuth usage response.
func ParseClaudeQuota(raw []byte) (*Quota, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}

	var fiveHour *float64
	type windowItem struct {
		key   string
		frac  float64
		reset time.Time
	}
	var weekly []windowItem

	for k, v := range payload {
		obj, ok := v.(map[string]any)
		if !ok {
			continue
		}
		rem, err := RemainingFromUsedPercent(obj["utilization"], false)
		if err != nil {
			continue
		}
		resetVal := obj["resets_at"]
		if resetVal == nil {
			resetVal = obj["resetsAt"]
		}
		rt, err := ParseResetTime(resetVal, nil)
		if err != nil {
			continue
		}

		if k == "five_hour" {
			fiveHour = &rem
		} else if strings.HasPrefix(k, "seven_day") || k == "iguana_necktie" {
			weekly = append(weekly, windowItem{key: k, frac: rem, reset: rt})
		}
	}

	if len(weekly) == 0 {
		return nil, errors.New("Claude seven-day quota windows are missing")
	}

	// Prefer primary "seven_day", otherwise earliest reset
	primary := weekly[0]
	for _, w := range weekly {
		if w.key == "seven_day" {
			primary = w
			break
		}
		if w.reset.Before(primary.reset) {
			primary = w
		}
	}

	longs := make([]float64, len(weekly))
	for i, w := range weekly {
		longs[i] = w.frac
	}

	return &Quota{
		FiveHourFraction: fiveHour,
		WeeklyFraction:   primary.frac,
		WeeklyReset:      primary.reset,
		WeeklyFractions:  longs,
		LastPolled:       time.Now().UTC(),
	}, nil
}

// ParseCodexQuota parses OpenAI ChatGPT rate-limit quota response.
func ParseCodexQuota(raw []byte) (*Quota, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}

	type rateWindow struct {
		hours float64
		reset time.Time
		rem   float64
	}

	parseRateObj := func(obj map[string]any) *rateWindow {
		if obj == nil {
			return nil
		}
		sec, ok := toFloat(obj["limit_window_seconds"])
		if !ok {
			sec, _ = toFloat(obj["limitWindowSeconds"])
		}
		if sec <= 0 {
			return nil
		}
		hours := sec / 3600.0

		rVal := obj["reset_at"]
		if rVal == nil {
			rVal = obj["resetAt"]
		}
		rAfter := obj["reset_after_seconds"]
		if rAfter == nil {
			rAfter = obj["resetAfterSeconds"]
		}
		rt, err := ParseResetTime(rVal, rAfter)
		if err != nil {
			return nil
		}

		reached, _ := obj["limit_reached"].(bool)
		allowed, hasAllowed := obj["allowed"].(bool)
		exhausted := reached || (hasAllowed && !allowed)

		usedVal := obj["used_percent"]
		if usedVal == nil {
			usedVal = obj["usedPercent"]
		}
		rem, err := RemainingFromUsedPercent(usedVal, exhausted)
		if err != nil {
			return nil
		}
		return &rateWindow{hours: hours, reset: rt, rem: rem}
	}

	windowsFromMap := func(m map[string]any) []rateWindow {
		var windows []rateWindow
		if m == nil {
			return windows
		}
		for _, k := range []string{"primary_window", "primaryWindow", "secondary_window", "secondaryWindow"} {
			if sub, ok := m[k].(map[string]any); ok {
				if rw := parseRateObj(sub); rw != nil {
					windows = append(windows, *rw)
				}
			}
		}
		return windows
	}

	var mainWindows []rateWindow
	if rl, ok := payload["rate_limit"].(map[string]any); ok {
		mainWindows = windowsFromMap(rl)
	} else if rl, ok := payload["rateLimit"].(map[string]any); ok {
		mainWindows = windowsFromMap(rl)
	}
	allWindows := append([]rateWindow(nil), mainWindows...)

	if rev, ok := payload["code_review_rate_limit"].(map[string]any); ok {
		allWindows = append(allWindows, windowsFromMap(rev)...)
	} else if rev, ok := payload["codeReviewRateLimit"].(map[string]any); ok {
		allWindows = append(allWindows, windowsFromMap(rev)...)
	}
	additional, _ := payload["additional_rate_limits"].([]any)
	if additional == nil {
		additional, _ = payload["additionalRateLimits"].([]any)
	}
	for _, item := range additional {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if rateLimit, ok := entry["rate_limit"].(map[string]any); ok {
			allWindows = append(allWindows, windowsFromMap(rateLimit)...)
		} else if rateLimit, ok := entry["rateLimit"].(map[string]any); ok {
			allWindows = append(allWindows, windowsFromMap(rateLimit)...)
		}
	}

	var fiveCandidates []rateWindow
	var mainLongWindows []rateWindow
	for _, w := range mainWindows {
		if w.hours <= 6.0 {
			fiveCandidates = append(fiveCandidates, w)
		} else {
			mainLongWindows = append(mainLongWindows, w)
		}
	}
	var allLongWindows []rateWindow
	for _, w := range allWindows {
		if w.hours > 6.0 {
			allLongWindows = append(allLongWindows, w)
		}
	}
	routingLongWindows := mainLongWindows
	if len(routingLongWindows) == 0 {
		routingLongWindows = allLongWindows
	}

	if len(routingLongWindows) == 0 {
		return nil, errors.New("Codex long quota window is missing")
	}

	// Primary long window: earliest reset
	primary := routingLongWindows[0]
	for _, w := range routingLongWindows[1:] {
		if w.reset.Before(primary.reset) {
			primary = w
		}
	}

	var fivePtr *float64
	if len(fiveCandidates) > 0 {
		minFive := fiveCandidates[0].rem
		for _, w := range fiveCandidates[1:] {
			if w.rem < minFive {
				minFive = w.rem
			}
		}
		fivePtr = &minFive
	}

	// Auxiliary model limits must not keep a credential routable for ordinary
	// Codex models once its main weekly limit is exhausted.
	longs := make([]float64, len(routingLongWindows))
	for i, w := range routingLongWindows {
		longs[i] = w.rem
	}

	return &Quota{
		FiveHourFraction: fivePtr,
		WeeklyFraction:   primary.rem,
		WeeklyReset:      primary.reset,
		WeeklyFractions:  longs,
		LastPolled:       time.Now().UTC(),
	}, nil
}

// ParseKimiQuota parses Kimi Coding usages response.
func ParseKimiQuota(raw []byte) (*Quota, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}

	limits, ok := payload["limits"].([]any)
	if !ok {
		return nil, errors.New("Kimi quota limits array missing")
	}

	type limitRow struct {
		hours float64
		reset time.Time
		rem   float64
	}
	var rows []limitRow

	for _, item := range limits {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		detail, _ := m["detail"].(map[string]any)
		if detail == nil {
			detail = m
		}
		window, _ := m["window"].(map[string]any)

		limit, _ := toFloat(detail["limit"])
		used, _ := toFloat(detail["used"])
		if limit <= 0 {
			continue
		}
		rem := math.Max(0.0, math.Min(1.0, (limit-used)/limit))

		rtVal := detail["reset_at"]
		if rtVal == nil {
			rtVal = detail["resetAt"]
		}
		rtIn := detail["reset_in"]
		if rtIn == nil {
			rtIn = detail["ttl"]
		}
		rt, err := ParseResetTime(rtVal, rtIn)
		if err != nil {
			continue
		}

		duration, _ := toFloat(window["duration"])
		unit := strings.ToLower(fmt.Sprintf("%v", window["timeUnit"]))
		hours := duration / 60.0
		if strings.Contains(unit, "second") {
			hours = duration / 3600.0
		} else if strings.Contains(unit, "hour") {
			hours = duration
		} else if strings.Contains(unit, "day") {
			hours = duration * 24.0
		} else if strings.Contains(unit, "week") {
			hours = duration * 168.0
		}

		rows = append(rows, limitRow{hours: hours, reset: rt, rem: rem})
	}

	var longRows []limitRow
	var shortRows []limitRow
	for _, r := range rows {
		if r.hours > 6.0 {
			longRows = append(longRows, r)
		} else {
			shortRows = append(shortRows, r)
		}
	}
	if len(longRows) == 0 {
		return nil, errors.New("Kimi long quota window is missing")
	}

	// Primary is longest duration
	primary := longRows[0]
	for _, r := range longRows[1:] {
		if r.hours > primary.hours {
			primary = r
		}
	}

	var fivePtr *float64
	if len(shortRows) > 0 {
		minShort := shortRows[0].rem
		for _, r := range shortRows[1:] {
			if r.rem < minShort {
				minShort = r.rem
			}
		}
		fivePtr = &minShort
	}

	longs := make([]float64, len(longRows))
	for i, r := range longRows {
		longs[i] = r.rem
	}

	return &Quota{
		FiveHourFraction: fivePtr,
		WeeklyFraction:   primary.rem,
		WeeklyReset:      primary.reset,
		WeeklyFractions:  longs,
		LastPolled:       time.Now().UTC(),
	}, nil
}

// ParseXAIQuota parses xAI billing credits response.
func ParseXAIQuota(raw []byte) (*Quota, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}

	config, _ := payload["config"].(map[string]any)
	if config == nil {
		config = payload
	}
	period, _ := config["currentPeriod"].(map[string]any)
	if period == nil {
		period = map[string]any{}
	}

	resetVal := period["end"]
	if resetVal == nil {
		resetVal = config["billingPeriodEnd"]
	}
	rt, err := ParseResetTime(resetVal, nil)
	if err != nil {
		return nil, fmt.Errorf("xAI billingPeriodEnd: %w", err)
	}

	usedPct, ok := toFloat(config["creditUsagePercent"])
	if !ok {
		limit, _ := toFloat(config["monthlyLimit"])
		used, _ := toFloat(config["used"])
		if limit > 0 {
			usedPct = math.Min(100.0, math.Max(0.0, (used/limit)*100.0))
			ok = true
		}
	}
	if !ok {
		return nil, errors.New("xAI quota percentage missing")
	}

	rem, err := RemainingFromUsedPercent(usedPct, false)
	if err != nil {
		return nil, err
	}

	fractions := []float64{rem}
	if productUsage, ok := config["productUsage"].([]any); ok {
		for _, p := range productUsage {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}
			if u, ok := toFloat(pm["usagePercent"]); ok {
				if r, err := RemainingFromUsedPercent(u, false); err == nil {
					fractions = append(fractions, r)
				}
			}
		}
	}

	return &Quota{
		FiveHourFraction: nil,
		WeeklyFraction:   rem,
		WeeklyReset:      rt,
		WeeklyFractions:  fractions,
		LastPolled:       time.Now().UTC(),
	}, nil
}
