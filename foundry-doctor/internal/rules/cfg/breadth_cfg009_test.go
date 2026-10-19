package cfg

import "testing"

// FND-CFG-009 version 5: the schedule/cronExpression/timeZone spelling of the
// azure.ai.routines beta.9 and beta.10 schemas is evaluated, and the cron value
// is judged even when the time zone is missing or unrecognised.
func TestCFG009ScheduleSpelling(t *testing.T) {
	routine := func(trig string) string {
		return "services:\n  r:\n    host: azure.ai.routine\n    triggers:\n      default:\n" + trig
	}
	sched := func(cron, tz string) string {
		out := "        type: schedule\n        cronExpression: \"" + cron + "\"\n"
		if tz != "" {
			out += "        timeZone: " + tz + "\n"
		}
		return out
	}
	rec := func(cron, tz string) string {
		out := "        type: recurring\n        cron_expression: \"" + cron + "\"\n"
		if tz != "" {
			out += "        time_zone: " + tz + "\n"
		}
		return out
	}
	runReviewCases(t, []reviewCase{
		{name: "schedule-valid-cron-passes", id: "FND-CFG-009", yaml: routine(sched("0 7 * * 1-5", "UTC"))},
		{name: "schedule-every-minute-too-frequent", id: "FND-CFG-009", yaml: routine(sched("* * * * *", "UTC")), want: 1, contains: "cronExpression"},
		{name: "schedule-four-minute-step-too-frequent", id: "FND-CFG-009", yaml: routine(sched("*/4 * * * *", "UTC")), want: 1, contains: "five minutes"},
		{name: "schedule-five-minute-step-passes", id: "FND-CFG-009", yaml: routine(sched("*/5 * * * *", "UTC"))},
		{name: "schedule-six-field-is-invalid", id: "FND-CFG-009", yaml: routine(sched("0 0 7 * * *", "UTC")), want: 1, contains: "5-field"},
		{name: "schedule-out-of-range-is-invalid", id: "FND-CFG-009", yaml: routine(sched("61 * * * *", "UTC")), want: 1, contains: "5-field"},
		{name: "schedule-reference-is-unresolved", id: "FND-CFG-009", yaml: routine(sched("${CRON}", "UTC")), skip: uncertainPrefix + "r: trigger default cronExpression is unresolved"},
		{name: "schedule-missing-timeZone-valid-cron-is-uncertain", id: "FND-CFG-009", yaml: routine(sched("0 7 * * 1-5", "")), skip: uncertainPrefix + "r: trigger default has no timeZone"},
		{name: "schedule-unknown-timeZone-is-uncertain", id: "FND-CFG-009", yaml: routine(sched("0 7 * * 1-5", "Mars/Olympus")), skip: "timeZone is not in bundled identifier data"},
		{name: "schedule-timeZone-reference-is-uncertain", id: "FND-CFG-009", yaml: routine(sched("0 7 * * 1-5", "${TZ}")), skip: "timeZone is unresolved"},
		{name: "schedule-timeZone-iana-passes", id: "FND-CFG-009", yaml: routine(sched("0 7 * * 1-5", "Europe/Dublin"))},
		{name: "schedule-bad-cron-and-missing-timeZone-still-finding", id: "FND-CFG-009", yaml: routine(sched("* * * * *", "")), want: 1, contains: "five minutes"},
		{name: "schedule-snake-case-key-is-uncertain", id: "FND-CFG-009", yaml: routine("        type: schedule\n        cron_expression: \"* * * * *\"\n"), skip: uncertainPrefix + "r: trigger default has type schedule with cron_expression"},
		{name: "schedule-without-cron-key-is-uncertain", id: "FND-CFG-009", yaml: routine("        type: schedule\n"), skip: "no cronExpression key"},
		{name: "recurring-with-camelCase-key-stays-uncertain", id: "FND-CFG-009", yaml: routine("        type: recurring\n        cronExpression: \"* * * * *\"\n        timeZone: UTC\n"), skip: uncertainPrefix + "r: recurring trigger default uses cronExpression"},
		{name: "recurring-bad-cron-missing-tz-is-now-a-finding", id: "FND-CFG-009", yaml: routine(rec("* * * * *", "")), want: 1, contains: "cron_expression"},
		{name: "recurring-missing-tz-valid-cron-uncertain", id: "FND-CFG-009", yaml: routine(rec("0 7 * * 1-5", "")), skip: uncertainPrefix + "r: trigger default has no time_zone"},
		{name: "recurring-unknown-tz-valid-cron-uncertain", id: "FND-CFG-009", yaml: routine(rec("0 7 * * 1-5", "Nowhere/Zone")), skip: "time_zone is not in bundled identifier data"},
		{name: "cron-names-are-uncertain", id: "FND-CFG-009", yaml: routine(sched("0 7 * * MON", "UTC")), skip: uncertainPrefix + "r: trigger default cron expression uses unsupported names"},
	})
}

func TestCFG009ScheduleKeysStable(t *testing.T) {
	y := "services:\n  r:\n    host: azure.ai.routine\n    triggers:\n      fast:\n        type: schedule\n        cronExpression: \"* * * * *\"\n        timeZone: UTC\n"
	res := run(t, find(Register(), "FND-CFG-009"), inputFor(t, y))
	if len(res.Findings) != 1 || res.Findings[0].Key != "cron-too-frequent:fast" {
		t.Fatalf("unexpected findings %+v", res.Findings)
	}
}
