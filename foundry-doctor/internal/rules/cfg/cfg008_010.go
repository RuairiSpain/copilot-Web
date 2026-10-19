package cfg

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armexpr"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// unconfirmedAllowListSpellings are alternative spellings of the allow list
// that no vendored azd schema or catalogued source confirms for azure.yaml.
// They are recognised only so the rule does not claim the list is omitted.
var unconfirmedAllowListSpellings = []string{"allowedTools", "allowed-tools", "tool_filter", "toolFilter"}

func evalCFG008(in *sdk.Input) sdk.Result {
	d, okd := loadDoc(in)
	if !okd {
		return skip(sdk.SkipInputUnavailable + ": azure.yaml unavailable")
	}
	var (
		fs        []sdk.Finding
		skips     []string
		evaluated bool
	)
	for _, s := range serviceList(d) {
		if s.Host == hostToolbox && has(s.Node, "endpoint") {
			skips = append(skips, s.Name+": toolbox endpoint-reuse mode")
			continue
		}
		for ti, t := range items(s.get("tools")) {
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
				if alias := firstPresent(t, unconfirmedAllowListSpellings); alias != "" {
					skips = append(skips, fmt.Sprintf("%s: uncertain: tools[%d] uses %s, a spelling no verified source confirms for azure.yaml; it was not evaluated", s.Name, ti, alias))
					continue
				}
				skips = append(skips, s.Name+": uncertain: direct inline MCP tool omits allowed_tools")
				continue
			}
			if allowed.Kind != yaml.SequenceNode {
				skips = append(skips, s.Name+": allowed_tools is not a literal sequence")
				continue
			}
			names := refNames(allowed)
			if len(names) == 0 {
				fs = append(fs, finding(d, s, allowed, fmt.Sprintf("mcp-allowed-tools-empty:tools[%d]", ti), "direct inline MCP tool has an empty allowed_tools list"))
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
			return skip(sdk.SkipNotApplicable + ": project has no direct inline MCP tools")
		}
		return skip(strings.Join(skips, "; "))
	}
	if len(skips) > 0 {
		return skip(strings.Join(skips, "; "))
	}
	return sdk.Result{}
}

func firstPresent(n *yaml.Node, keys []string) string {
	for _, k := range keys {
		if has(n, k) {
			return k
		}
	}
	return ""
}

func evalCFG009(in *sdk.Input) sdk.Result {
	d, okd := loadDoc(in)
	if !okd {
		return skip(sdk.SkipInputUnavailable + ": azure.yaml unavailable")
	}
	st := cfgState{}
	declared := false
	for _, s := range serviceList(d) {
		if strings.ToLower(s.Host) != "azure.ai.routine" {
			continue
		}
		// Only a mapping of named triggers under "triggers" is understood
		// (the shape in the catalogue examples). The shape azd accepts is not
		// verified, so a list, a scalar or a singular "trigger" key is an
		// uncertain note, never not-applicable.
		if tn := s.get("triggers"); tn != nil && tn.Tag != "!!null" && tn.Kind != yaml.MappingNode {
			declared = true
			st.skips = append(st.skips, s.Name+": triggers is not a mapping of named triggers; that shape is not verified and was not evaluated")
		}
		if tn := s.get("trigger"); tn != nil && tn.Tag != "!!null" {
			declared = true
			st.skips = append(st.skips, s.Name+": uses a trigger key instead of triggers; that shape is not verified and was not evaluated")
		}
		for _, trig := range pairs(s.get("triggers")) {
			declared = true
			checkRoutineTrigger(d, s, trig.Key, trig.Val, &st)
		}
	}
	if !declared {
		return skip(sdk.SkipNotApplicable + ": no azure.ai.routine service declares triggers")
	}
	return withNotes(st.fs, st.skips)
}

// routineSpelling names the keys of one verified trigger shape.
type routineSpelling struct {
	cron, tz string
}

var (
	// azure.ai.routines 1.0.0-beta.8 schema: type recurring.
	spellingBeta8 = routineSpelling{cron: "cron_expression", tz: "time_zone"}
	// azure.ai.routines 1.0.0-beta.9 and 1.0.0-beta.10 schemas (and main): type
	// schedule with camelCase keys; the same schemas reject the snake_case keys.
	spellingBeta10 = routineSpelling{cron: "cronExpression", tz: "timeZone"}
)

// checkRoutineTrigger evaluates one trigger. Two shapes are understood: type
// recurring with cron_expression and time_zone (azure.ai.routines beta.8), and
// type schedule with cronExpression and timeZone (beta.9, beta.10 and main
// schemas). The cadence facts (five-field expression, at least five minutes,
// IANA or Windows time zone) come from the Learn routines article, which names
// the trigger a schedule trigger; the schemas type the cron value as a plain
// string. A mixed spelling, a cron key, or any other type is an uncertain note,
// never not-applicable.
func checkRoutineTrigger(d document, s service, key, trig *yaml.Node, st *cfgState) {
	typNode := child(trig, "type")
	typ := strings.ToLower(strings.TrimSpace(scalar(typNode)))
	if typ == "" || containsRef(typ) {
		st.skips = append(st.skips, s.Name+": trigger "+key.Value+" type is missing or unresolved")
		return
	}
	switch typ {
	case "recurring":
		sp := spellingBeta8
		if child(trig, sp.cron) == nil {
			switch {
			case child(trig, "cronExpression") != nil:
				st.skips = append(st.skips, s.Name+": recurring trigger "+key.Value+" uses cronExpression, which the azure.ai.routines beta.9 and later schemas pair with type schedule, not recurring; it was not evaluated")
			case child(trig, "cron") != nil:
				st.skips = append(st.skips, s.Name+": recurring trigger "+key.Value+" uses cron instead of cron_expression")
			default:
				st.skips = append(st.skips, s.Name+": recurring trigger "+key.Value+" has no cron_expression key the rule understands")
			}
			return
		}
		checkRoutineCron(d, s, key, trig, sp, st)
	case "schedule":
		sp := spellingBeta10
		if child(trig, sp.cron) == nil {
			switch {
			case child(trig, "cron_expression") != nil:
				st.skips = append(st.skips, s.Name+": trigger "+key.Value+" has type schedule with cron_expression; the azure.ai.routines beta.9 and later schemas reject snake_case keys, so it was not evaluated")
			default:
				st.skips = append(st.skips, s.Name+": trigger "+key.Value+" has type schedule and no cronExpression key the rule understands")
			}
			return
		}
		checkRoutineCron(d, s, key, trig, sp, st)
	default:
		st.skips = append(st.skips, s.Name+": trigger "+key.Value+" type "+clip(typ)+" is not evaluated by this rule")
	}
}

// checkRoutineCron validates the cron value and the time zone of one trigger
// whose cron key is present. The cron value is judged whether or not the time
// zone is set; a missing or unrecognised time zone is an uncertain note.
func checkRoutineCron(d document, s service, key, trig *yaml.Node, sp routineSpelling, st *cfgState) {
	cronNode := child(trig, sp.cron)
	cron := strings.TrimSpace(scalar(cronNode))
	if cron == "" || containsRef(cron) {
		st.skips = append(st.skips, s.Name+": trigger "+key.Value+" "+sp.cron+" is unresolved")
		return
	}
	if tz := strings.TrimSpace(scalar(child(trig, sp.tz))); tz == "" {
		st.skips = append(st.skips, s.Name+": trigger "+key.Value+" has no "+sp.tz)
	} else if !containsRef(tz) && !validRoutineTimeZone(tz) {
		st.skips = append(st.skips, s.Name+": trigger "+key.Value+" "+sp.tz+" is not in bundled identifier data")
	} else if containsRef(tz) {
		st.skips = append(st.skips, s.Name+": trigger "+key.Value+" "+sp.tz+" is unresolved")
	}
	expr, minGap, uncertain := parseRoutineCron(cron)
	if uncertain != "" {
		st.skips = append(st.skips, s.Name+": trigger "+key.Value+" "+uncertain)
		return
	}
	st.evaluated = true
	if expr == nil {
		st.fs = append(st.fs, finding(d, s, cronNode, "cron-invalid:"+key.Value, fmt.Sprintf("trigger %q %s %q is not a valid 5-field cron expression", key.Value, sp.cron, cron)))
		return
	}
	if minGap < 5 {
		st.fs = append(st.fs, finding(d, s, cronNode, "cron-too-frequent:"+key.Value, fmt.Sprintf("trigger %q %s %q fires more often than every five minutes", key.Value, sp.cron, cron)))
	}
}

func evalCFG010(in *sdk.Input) sdk.Result {
	var fs []sdk.Finding
	evaluated := false
	d, loaded := loadDoc(in)
	if loaded {
		for _, s := range serviceList(d) {
			if s.Host != hostProject {
				continue
			}
			for di, dep := range items(s.get("deployments")) {
				depKey := str(dep, "name")
				if depKey == "" || containsRef(depKey) {
					depKey = fmt.Sprintf("deployments[%d]", di)
				}
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
					fs = append(fs, finding(d, s, versionNode, "deployment-version-unpinned:"+depKey, "deployment model.version is empty or unpinned"))
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
					Key:      "deployment-version-unpinned",
					Resource: sdk.ResourceRef{Type: r.Type, Name: armexpr.StripScope(r.Name)},
					Location: r.Location,
					Evidence: "deployment model.version is empty or unpinned",
				})
			}
			if opt, ok := armGetString(r.Properties, "versionUpgradeOption"); ok {
				if !strings.HasPrefix(strings.TrimSpace(opt), "[") && strings.EqualFold(strings.TrimSpace(opt), "OnceNewDefaultVersionAvailable") {
					fs = append(fs, sdk.Finding{
						Key:      "upgrade-option-once-new-default",
						Resource: sdk.ResourceRef{Type: r.Type, Name: armexpr.StripScope(r.Name)},
						Location: r.Location,
						Evidence: "deployment versionUpgradeOption is OnceNewDefaultVersionAvailable",
					})
				}
			} else {
				fs = append(fs, sdk.Finding{
					Key:      "upgrade-option-absent",
					Resource: sdk.ResourceRef{Type: r.Type, Name: armexpr.StripScope(r.Name)},
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
		// Without the ARM model the deployments may be declared in Bicep that
		// could not be read, so "declares no deployments" is not known.
		if in == nil || in.ARM == nil {
			return skip(sdk.SkipInputUnavailable + ": compiled ARM model unavailable")
		}
		return skip(sdk.SkipNotApplicable + ": project declares no model deployments")
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
