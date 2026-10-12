package cfg

import (
	"context"
	"fmt"
	"net/netip"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// rule007 is FND-CFG-007 (product opinion): outbound endpoints use https and avoid loopback, link-local,
// metadata and private literals. It parses only; it never resolves DNS and never echoes a URL.
type rule007 struct{}

func (rule007) ID() string   { return "FND-CFG-007" }
func (rule007) Version() int { return 1 }

const (
	rec007 = "Use https endpoints reachable from the service that calls them. For internal servers, publish them on a private endpoint or an internal Container Apps ingress and use their private DNS name over https instead of an IP literal. Remove localhost and metadata addresses from deployed configuration."
	fix007 = "services:\n  github-conn:\n    host: azure.ai.connection\n    category: RemoteTool\n    target: https://api.githubcopilot.com/mcp\n"
)

func (r rule007) Evaluate(_ context.Context, in *model.Input) ([]rules.Result, error) {
	if yamlMissing(in) {
		return skipNoYAML(r), nil
	}
	x := newRun(r, in)
	for _, f := range append(urlFields(in.AzureYAML), envURLFields(in.AzureYAML)...) {
		if wholeRef(f.value) || strings.HasPrefix(f.value, "${") {
			x.skip(rules.ReasonUnresolvedValue("url-is-env-reference"), "expanded ${VAR} values", f.svc.Name+"."+f.name)
			continue
		}
		u, ok := parseURL(f.value)
		if !ok {
			continue // an ARM resource ID or other non-URL value
		}
		if !strings.EqualFold(u.Scheme, "https") {
			x.fail(svcRef(f.svc), x.loc(f.pos), "scheme:"+f.svc.Name+":"+f.name, sdk.ConfidenceCertain,
				fmt.Sprintf("%s of service %s uses scheme %q instead of https", f.name, f.svc.Name, strings.ToLower(u.Scheme)), rec007, fix007)
		}
		if class := hostClass(u.Hostname()); class != "" {
			x.fail(svcRef(f.svc), x.loc(f.pos), "host:"+f.svc.Name+":"+f.name+":"+class, sdk.ConfidenceCertain,
				fmt.Sprintf("%s of service %s targets a %s address", f.name, f.svc.Name, class), rec007, fix007)
		}
	}
	return x.done(), nil
}

// hostClass classifies a host name or IP literal. It returns "" for ordinary names and for hosts that
// contain a (masked) reference.
func hostClass(host string) string {
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	if strings.Contains(h, refToken) {
		return ""
	}
	switch h {
	case "localhost":
		return "loopback"
	case "metadata.google.internal", "metadata.goog":
		return "metadata"
	}
	if strings.HasSuffix(h, ".localhost") {
		return "loopback"
	}
	ip, err := netip.ParseAddr(h)
	if err != nil {
		return ""
	}
	ip = ip.Unmap()
	switch {
	case ip == netip.MustParseAddr("169.254.169.254") || ip == netip.MustParseAddr("fd00:ec2::254"):
		return "metadata"
	case ip.IsLoopback() || ip.IsUnspecified():
		return "loopback"
	case ip.IsLinkLocalUnicast():
		return "link-local"
	case ip.IsPrivate():
		return "private"
	}
	return ""
}
