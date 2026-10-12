package sec

import (
	"context"
	"regexp"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// rule004 is FND-SEC-004: Storage accounts used by Foundry block Shared Key and anonymous access and
// require HTTPS and TLS 1.2. Each of the four sub-checks reports separately.
type rule004 struct{}

func (rule004) ID() string   { return "FND-SEC-004" }
func (rule004) Version() int { return 1 }

func (r rule004) Evaluate(ctx context.Context, in *model.Input) ([]rules.Result, error) {
	return run(ctx, r, in)
}

const (
	rec004 = "Set allowSharedKeyAccess false, allowBlobPublicAccess false, supportsHttpsTrafficOnly true and minimumTlsVersion TLS1_2 on the account, and give the Foundry identity Storage data roles. Verify no client still uses account keys or SAS signed with the account key before disabling Shared Key."
	fix004 = "resource st 'Microsoft.Storage/storageAccounts@2026-06-01' = {\n  properties: {\n    allowSharedKeyAccess: false\n    allowBlobPublicAccess: false\n    supportsHttpsTrafficOnly: true\n    minimumTlsVersion: 'TLS1_2'\n  }\n}"

	// httpsDefaultSince is the API version from which supportsHttpsTrafficOnly defaults to true on create
	// (storage 2026-06-01 specification text).
	httpsDefaultSince = "2019-04-01"
)

var apiDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}`)

func (r rule004) evaluateARM(in *model.Input) []rules.Result {
	var out []rules.Result
	ix := buildIndex(in.ARM)
	for _, res := range in.ARM.ResourcesOfType(typStorage) {
		sc, d, skipped := scoped(r, ix, res, storageKind)
		if skipped != nil {
			out = append(out, *skipped)
			continue
		}
		tail := "; " + scopeText(sc) + " (apiVersion " + safeAPI(res.APIVersion) + ")"
		fail := func(key, prop, ev string, dd doubt) rules.Result {
			return finding(r, res, dd, key, prop, prop+" "+ev+tail, rec004, fix004)
		}

		// allowSharedKeyAccess: only an explicit false passes; null is equivalent to true.
		const shared = "properties.allowSharedKeyAccess"
		switch st, v := boolOf(res, shared); st {
		case "false":
			out = append(out, rules.Pass())
		case "unresolved":
			out = append(out, unresolvedSkip(r, res, shared, v))
		case "invalid":
			out = append(out, invalidSkip(r, res, shared))
		case "absent":
			out = append(out, fail("allowSharedKeyAccess", shared, "is absent, which is equivalent to true (Shared Key allowed)", d))
		default:
			out = append(out, fail("allowSharedKeyAccess", shared, "is true (Shared Key allowed)", d))
		}

		// allowBlobPublicAccess: only an explicit true fails; the specification default is false.
		const pub = "properties.allowBlobPublicAccess"
		switch st, v := boolOf(res, pub); st {
		case "true":
			out = append(out, fail("allowBlobPublicAccess", pub, "is true (anonymous blob access allowed)", d))
		case "unresolved":
			out = append(out, unresolvedSkip(r, res, pub, v))
		case "invalid":
			out = append(out, invalidSkip(r, res, pub))
		default:
			out = append(out, rules.Pass())
		}

		// supportsHttpsTrafficOnly: false fails; absent is true from 2019-04-01 on.
		const https = "properties.supportsHttpsTrafficOnly"
		switch st, v := boolOf(res, https); st {
		case "true":
			out = append(out, rules.Pass())
		case "false":
			out = append(out, fail("supportsHttpsTrafficOnly", https, "is false (plain HTTP allowed)", d))
		case "unresolved":
			out = append(out, unresolvedSkip(r, res, https, v))
		case "invalid":
			out = append(out, invalidSkip(r, res, https))
		default:
			if apiDate.MatchString(res.APIVersion) && res.APIVersion[:10] >= httpsDefaultSince {
				out = append(out, rules.Pass())
			} else {
				u := doubt{uncertain: true, conf: sdk.ConfidenceUncertain,
					why: "apiVersion " + safeAPI(res.APIVersion) + " is not known to default supportsHttpsTrafficOnly to true"}
				out = append(out, fail("supportsHttpsTrafficOnly", https, "is absent", u))
			}
		}

		// minimumTlsVersion: TLS1_2 and TLS1_3 pass; the specification default for an absent value is TLS1_0.
		const tls = "properties.minimumTlsVersion"
		v := res.Get(tls)
		switch s, isStr := v.String(); {
		case v.State == model.Unresolved:
			out = append(out, unresolvedSkip(r, res, tls, v))
		case v.State == model.Absent:
			out = append(out, fail("minimumTlsVersion", tls, "is absent, so the default TLS1_0 applies", d))
		case isStr && (strings.EqualFold(s, "TLS1_2") || strings.EqualFold(s, "TLS1_3")):
			out = append(out, rules.Pass())
		case isStr && (strings.EqualFold(s, "TLS1_0") || strings.EqualFold(s, "TLS1_1")):
			out = append(out, fail("minimumTlsVersion", tls, "is "+strings.ToUpper(s)+" (TLS 1.2 or later required)", d))
		default:
			out = append(out, invalidSkip(r, res, tls))
		}
	}
	return out
}

func safeAPI(v string) string {
	if apiDate.MatchString(v) && isSafeToken(v) {
		return v
	}
	return "(unknown)"
}
