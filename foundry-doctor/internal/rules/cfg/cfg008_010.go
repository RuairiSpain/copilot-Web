package cfg

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func evalCFG008(in *sdk.Input) sdk.Result {
	d, okd := loadDoc(in)
	if !okd {
		return skip(sdk.SkipInputUnavailable)
	}
	var (
		fs        []sdk.Finding
		skips     []string
		evaluated bool
	)
	for _, s := range serviceList(d) {
		if s.Host == hostToolbox {
			if len(items(s.get("tools"))) > 0 {
				skips = append(skips, s.Name+": toolbox endpoint-reuse mode")
			}
			continue
		}
		for _, t := range items(s.get("tools")) {
			if child(t, "$ref") != nil {
				skips = append(skips, s.Name+": tool definition comes from $ref")
				continue
			}
			tt := strings.ToLower(strings.TrimSpace(str(t, "type")))
			if tt == "" || tt != "mcp" {
				continue
			}
			evaluated = true
			allowed := child(t, "allowed_tools")
			if allowed == nil {
				skips = append(skips, s.Name+": uncertain: direct inline MCP tool omits allowed_tools")
				continue
			}
			if allowed.Kind != yaml.SequenceNode {
				skips = append(skips, s.Name+": allowed_tools is not a literal sequence")
				continue
			}
			names := refNames(allowed)
			if len(names) == 0 {
				fs = append(fs, finding(d, s, allowed, "direct inline MCP tool has an empty allowed_tools list"))
				continue
			}
			nonEmpty := false
			for _, n := range names {
				if n.Name != "" && !containsRef(n.Name) {
					nonEmpty = true
				}
			}
			if !nonEmpty {
				skips = append(skips, s.Name+": allowed_tools is unresolved")
			}
		}
	}
	if len(fs) > 0 {
		return ok(fs)
	}
	if !evaluated {
		if len(skips) == 0 {
			return skip(sdk.SkipInputUnavailable)
		}
		return skip(strings.Join(skips, "; "))
	}
	if len(skips) > 0 {
		return skip(strings.Join(skips, "; "))
	}
	return sdk.Result{}
}

func evalCFG009(in *sdk.Input) sdk.Result {
	d, okd := loadDoc(in)
	if !okd {
		return skip(sdk.SkipInputUnavailable)
	}
	st := cfgState{}
	for _, s := range serviceList(d) {
		if strings.ToLower(s.Host) != "azure.ai.routine" {
			continue
		}
		for _, trig := range pairs(s.get("triggers")) {
			checkRoutineTrigger(d, s, trig.Key, trig.Val, &st)
		}
	}
	if len(st.fs) > 0 {
		return ok(st.fs)
	}
	if !st.evaluated {
		if len(st.skips) == 0 {
			return skip(sdk.SkipInputUnavailable)
		}
		return skip(strings.Join(st.skips, "; "))
	}
	if len(st.skips) > 0 {
		return skip(strings.Join(st.skips, "; "))
	}
	return sdk.Result{}
}

func checkRoutineTrigger(d document, s service, key, trig *yaml.Node, st *cfgState) {
	typNode := child(trig, "type")
	typ := strings.ToLower(strings.TrimSpace(scalar(typNode)))
	if typ == "" || containsRef(typ) {
		st.skips = append(st.skips, s.Name+": trigger "+key.Value+" type is unresolved")
		return
	}
	switch typ {
	case "recurring":
		cronNode := child(trig, "cron_expression")
		if cronNode == nil {
			if child(trig, "cron") != nil {
				st.skips = append(st.skips, s.Name+": uncertain: recurring trigger "+key.Value+" uses cron instead of cron_expression")
				return
			}
			return
		}
		cron := strings.TrimSpace(scalar(cronNode))
		if cron == "" || containsRef(cron) {
			st.skips = append(st.skips, s.Name+": trigger "+key.Value+" cron_expression is unresolved")
			return
		}
		tzNode := child(trig, "time_zone")
		if tzNode == nil || strings.TrimSpace(scalar(tzNode)) == "" {
			st.skips = append(st.skips, s.Name+": uncertain: recurring trigger "+key.Value+" has no time_zone")
			return
		}
		if !validRoutineTimeZone(strings.TrimSpace(scalar(tzNode))) {
			st.skips = append(st.skips, s.Name+": uncertain: recurring trigger "+key.Value+" time_zone is not in bundled identifier data")
			return
		}
		expr, minGap, uncertain := parseRoutineCron(cron)
		if uncertain != "" {
			st.skips = append(st.skips, s.Name+": uncertain: trigger "+key.Value+" "+uncertain)
			return
		}
		st.evaluated = true
		if expr == nil {
			st.fs = append(st.fs, finding(d, s, cronNode, fmt.Sprintf("trigger %q cron_expression %q is not a valid 5-field cron expression", key.Value, cron)))
			return
		}
		if minGap < 5 {
			st.fs = append(st.fs, finding(d, s, cronNode, fmt.Sprintf("trigger %q cron_expression %q fires more often than every five minutes", key.Value, cron)))
		}
	case "schedule":
		if child(trig, "cron_expression") != nil {
			st.skips = append(st.skips, s.Name+": uncertain: trigger "+key.Value+" uses schedule with cron_expression")
		}
	default:
		st.skips = append(st.skips, s.Name+": trigger "+key.Value+" type "+typ+" is out of scope")
	}
}

func evalCFG010(in *sdk.Input) sdk.Result {
	var fs []sdk.Finding
	evaluated := false
	if d, okd := loadDoc(in); okd {
		for _, s := range serviceList(d) {
			if s.Host != hostProject {
				continue
			}
			for _, dep := range items(s.get("deployments")) {
				model := child(dep, "model")
				name := strings.ToLower(strings.TrimSpace(str(model, "name")))
				if strings.Contains(name, "router") {
					continue
				}
				versionNode := child(model, "version")
				version := strings.TrimSpace(scalar(versionNode))
				if versionNode != nil && containsRef(version) {
					continue
				}
				evaluated = true
				if version == "" || looksUnpinnedVersion(version) {
					fs = append(fs, finding(d, s, versionNode, "deployment model.version is empty or unpinned"))
				}
			}
		}
	}
	if in != nil && in.ARM != nil {
		for _, r := range in.ARM.Resources() {
			if !strings.EqualFold(r.Type, "Microsoft.CognitiveServices/accounts/deployments") {
				continue
			}
			name, _ := armGet(r.Properties, "model", "name")
			if strings.Contains(strings.ToLower(fmt.Sprint(name)), "router") {
				continue
			}
			evaluated = true
			v, vok := armGetString(r.Properties, "model", "version")
			if !vok || strings.TrimSpace(v) == "" || looksUnpinnedVersion(v) {
				fs = append(fs, sdk.Finding{
					Resource: sdk.ResourceRef{Type: r.Type, Name: r.Name},
					Location: r.Location,
					Evidence: "deployment model.version is empty or unpinned",
				})
			}
			if opt, ok := armGetString(r.Properties, "versionUpgradeOption"); ok {
				if !strings.HasPrefix(strings.TrimSpace(opt), "[") && strings.EqualFold(strings.TrimSpace(opt), "OnceNewDefaultVersionAvailable") {
					fs = append(fs, sdk.Finding{
						Resource: sdk.ResourceRef{Type: r.Type, Name: r.Name},
						Location: r.Location,
						Evidence: "deployment versionUpgradeOption is OnceNewDefaultVersionAvailable",
					})
				}
			} else {
				fs = append(fs, sdk.Finding{
					Resource: sdk.ResourceRef{Type: r.Type, Name: r.Name},
					Location: r.Location,
					Evidence: "deployment versionUpgradeOption is absent",
				})
			}
		}
	}
	if len(fs) > 0 {
		return ok(fs)
	}
	if !evaluated {
		return skip(sdk.SkipInputUnavailable)
	}
	return sdk.Result{}
}

func looksUnpinnedVersion(v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "", "latest", "default", "current":
		return true
	}
	return strings.Contains(v, "${") || strings.Contains(v, "latest")
}

type cronExpr struct {
	minutes map[int]bool
	hours   map[int]bool
	days    map[int]bool
	months  map[int]bool
	dows    map[int]bool
}

func parseRoutineCron(s string) (*cronExpr, int, string) {
	fields := strings.Fields(strings.TrimSpace(s))
	if len(fields) != 5 {
		return nil, 0, ""
	}
	mins, unc, ok := parseCronField(fields[0], 0, 59)
	if unc != "" {
		return nil, 0, unc
	}
	if !ok {
		return nil, 0, ""
	}
	hours, unc, ok := parseCronField(fields[1], 0, 23)
	if unc != "" {
		return nil, 0, unc
	}
	if !ok {
		return nil, 0, ""
	}
	days, unc, ok := parseCronField(fields[2], 1, 31)
	if unc != "" {
		return nil, 0, unc
	}
	if !ok {
		return nil, 0, ""
	}
	months, unc, ok := parseCronField(fields[3], 1, 12)
	if unc != "" {
		return nil, 0, unc
	}
	if !ok {
		return nil, 0, ""
	}
	dows, unc, ok := parseCronField(fields[4], 0, 6)
	if unc != "" {
		return nil, 0, unc
	}
	if !ok {
		return nil, 0, ""
	}
	expr := &cronExpr{minutes: mins, hours: hours, days: days, months: months, dows: dows}
	return expr, cronMinGap(expr), ""
}

func parseCronField(field string, min, max int) (map[int]bool, string, bool) {
	if strings.ContainsAny(field, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyzL#?") {
		return nil, "cron expression uses unsupported names or extensions", false
	}
	out := map[int]bool{}
	for _, part := range strings.Split(field, ",") {
		vals, unc, ok := parseCronPart(part, min, max)
		if unc != "" {
			return nil, unc, false
		}
		if !ok {
			return nil, "", false
		}
		for _, v := range vals {
			out[v] = true
		}
	}
	return out, "", len(out) > 0
}

func parseCronPart(part string, min, max int) ([]int, string, bool) {
	part = strings.TrimSpace(part)
	if part == "*" {
		return span(min, max, 1), "", true
	}
	base, step := part, 1
	if strings.Contains(part, "/") {
		parts := strings.Split(part, "/")
		if len(parts) != 2 {
			return nil, "", false
		}
		base = parts[0]
		n, err := strconv.Atoi(parts[1])
		if err != nil || n <= 0 {
			return nil, "", false
		}
		step = n
	}
	if base == "*" {
		return span(min, max, step), "", true
	}
	if strings.Contains(base, "-") {
		bits := strings.Split(base, "-")
		if len(bits) != 2 {
			return nil, "", false
		}
		start, err1 := strconv.Atoi(bits[0])
		end, err2 := strconv.Atoi(bits[1])
		if err1 != nil || err2 != nil || start < min || end > max || start > end {
			return nil, "", false
		}
		return span(start, end, step), "", true
	}
	n, err := strconv.Atoi(base)
	if err != nil || n < min || n > max {
		return nil, "", false
	}
	return []int{n}, "", true
}

func span(start, end, step int) []int {
	var out []int
	for i := start; i <= end; i += step {
		out = append(out, i)
	}
	return out
}

func cronMinGap(expr *cronExpr) int {
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	var prev *time.Time
	minGap := 1<<31 - 1
	for i := 0; i < 8*24*60; i++ {
		t := start.Add(time.Duration(i) * time.Minute)
		if !expr.minutes[t.Minute()] || !expr.hours[t.Hour()] || !expr.days[t.Day()] || !expr.months[int(t.Month())] || !expr.dows[int(t.Weekday())] {
			continue
		}
		if prev != nil {
			gap := int(t.Sub(*prev).Minutes())
			if gap < minGap {
				minGap = gap
			}
		}
		cp := t
		prev = &cp
	}
	if minGap == 1<<31-1 {
		return 0
	}
	return minGap
}

func validRoutineTimeZone(v string) bool {
	if _, err := time.LoadLocation(v); err == nil {
		return true
	}
	switch v {
	case "UTC", "Coordinated Universal Time", "Pacific Standard Time", "Eastern Standard Time":
		return true
	default:
		return false
	}
}

func armGet(m map[string]any, path ...string) (any, bool) {
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = mm[p]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func armGetString(m map[string]any, path ...string) (string, bool) {
	v, ok := armGet(m, path...)
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}
