package compare_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/compare"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/project"
)

func env(name string, kv map[string]string) model.Environment { return model.NewEnvironment(name, kv) }

func kinds(d compare.Diff) map[string]compare.DiffKind {
	m := map[string]compare.DiffKind{}
	for _, k := range d.Keys {
		m[k.Key] = k.Kind
	}
	return m
}

func TestEnvironments(t *testing.T) {
	canaryL := "canary-left-" + strings.Repeat("x", 5)
	canaryR := "canary-right-" + strings.Repeat("y", 5)
	tests := []struct {
		name        string
		left, right map[string]string
		want        map[string]compare.DiffKind
		identical   bool
	}{
		{"same", map[string]string{"A": "1", "B": "2"}, map[string]string{"A": "1", "B": "2"},
			map[string]compare.DiffKind{"A": compare.Same, "B": compare.Same}, true},
		{"different", map[string]string{"A": canaryL}, map[string]string{"A": canaryR},
			map[string]compare.DiffKind{"A": compare.Different}, false},
		{"missing key on right", map[string]string{"A": "1", "B": "2"}, map[string]string{"A": "1"},
			map[string]compare.DiffKind{"A": compare.Same, "B": compare.OnlyLeft}, false},
		{"extra key on right", map[string]string{"A": "1"}, map[string]string{"A": "1", "C": "3"},
			map[string]compare.DiffKind{"A": compare.Same, "C": compare.OnlyRight}, false},
		{"empty values equal", map[string]string{"A": ""}, map[string]string{"A": ""},
			map[string]compare.DiffKind{"A": compare.Same}, true},
		{"empty differs from missing", map[string]string{"A": ""}, map[string]string{},
			map[string]compare.DiffKind{"A": compare.OnlyLeft}, false},
		{"empty differs from value", map[string]string{"A": ""}, map[string]string{"A": "x"},
			map[string]compare.DiffKind{"A": compare.Different}, false},
		{"both empty", map[string]string{}, map[string]string{}, map[string]compare.DiffKind{}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, err := compare.Environments(context.Background(), env("l", tc.left), env("r", tc.right))
			if err != nil {
				t.Fatal(err)
			}
			got := kinds(d)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
			for k, w := range tc.want {
				if got[k] != w {
					t.Errorf("%s: got %s want %s", k, got[k], w)
				}
			}
			if d.Identical() != tc.identical {
				t.Errorf("Identical = %v", d.Identical())
			}
			out, _ := json.Marshal(d)
			s := string(out) + fmt.Sprintf("%v %+v %#v", d, d, d)
			for _, c := range []string{canaryL, canaryR} {
				if strings.Contains(s, c) {
					t.Fatalf("value leaked into output")
				}
			}
		})
	}
}

func TestNoValueHashesInOutput(t *testing.T) {
	d, err := compare.Environments(context.Background(), env("l", map[string]string{"REGION": "westeurope"}), env("r", map[string]string{"REGION": "northeurope"}))
	if err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(d)
	if strings.Contains(strings.ToLower(string(out)), "fingerprint") {
		t.Fatalf("diff must not carry fingerprints: %s", out)
	}
	if d.Keys[0].Kind != compare.Different {
		t.Fatal("expected different")
	}
}

func TestSortedAndScope(t *testing.T) {
	l := env("l", map[string]string{"Z": "1", "AZURE_SUBSCRIPTION_ID": "s1", "AZURE_RESOURCE_GROUP": "rg", "A": "1"})
	r := env("r", map[string]string{"AZURE_SUBSCRIPTION_ID": "s2", "AZURE_RESOURCE_GROUP": "rg", "M": "1"})
	d, err := compare.Environments(context.Background(), l, r)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, k := range d.Keys {
		names = append(names, k.Key)
	}
	if strings.Join(names, ",") != "A,AZURE_RESOURCE_GROUP,AZURE_SUBSCRIPTION_ID,M,Z" {
		t.Fatalf("order %v", names)
	}
	if d.Subscription != compare.Different || d.ResourceGroup != compare.Same {
		t.Fatalf("scope %s %s", d.Subscription, d.ResourceGroup)
	}
	d2, _ := compare.Environments(context.Background(), env("a", nil), env("b", nil))
	if d2.Subscription != "" || d2.ResourceGroup != "" {
		t.Fatal("scope should be empty when neither side sets it")
	}
	d3, _ := compare.Environments(context.Background(), l, r)
	a, _ := json.Marshal(d)
	b, _ := json.Marshal(d3)
	if string(a) != string(b) {
		t.Fatal("not deterministic")
	}
}

func TestCRLFThroughParser(t *testing.T) {
	ctx := context.Background()
	lf, err := project.ParseEnv(ctx, "a", []byte("A=one\nB=two\n"))
	if err != nil {
		t.Fatal(err)
	}
	crlf, err := project.ParseEnv(ctx, "b", []byte("A=one\r\nB=two\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	d, err := compare.Environments(ctx, lf.Env, crlf.Env)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Identical() {
		t.Fatalf("CRLF and LF should compare equal: %v", d.Keys)
	}
}

func TestEnvironmentBound(t *testing.T) {
	tests := []struct {
		key, val string
		want     bool
	}{
		{"AZURE_SUBSCRIPTION_ID", "x", true},
		{"AZURE_RESOURCE_GROUP", "x", true},
		{"AZURE_AI_ACCOUNT_NAME", "x", true},
		{"AZURE_TENANT_ID", "x", false},
		{"AZURE_LOCATION", "westeurope", false},
		{"SOMETHING", "/subscriptions/s/resourceGroups/rg", true},
		{"SOMETHING", "https://a.Services.AI.azure.com/api", true},
		{"SOMETHING", "plain", false},
	}
	for _, tc := range tests {
		if got := compare.EnvironmentBound(tc.key, model.NewEnvValue(tc.val)); got != tc.want {
			t.Errorf("%s=%q got %v", tc.key, tc.val, got)
		}
	}
}

func TestBoundFlagInDiff(t *testing.T) {
	d, _ := compare.Environments(context.Background(), env("l", map[string]string{"X_NAME": "a", "Y": "a"}), env("r", map[string]string{"X_NAME": "b", "Y": "b"}))
	for _, k := range d.Keys {
		if k.Bound != (k.Key == "X_NAME") {
			t.Errorf("bound flag wrong for %s", k.Key)
		}
	}
}

func TestLargeEnvironment(t *testing.T) {
	l, r := map[string]string{}, map[string]string{}
	for i := range 20000 {
		k := fmt.Sprintf("KEY_%05d", i)
		l[k], r[k] = "v", "v"
		if i%100 == 0 {
			r[k] = "w"
		}
	}
	d, err := compare.Environments(context.Background(), env("l", l), env("r", r))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Keys) != 20000 || d.Count(compare.Different) != 200 || d.Count(compare.Same) != 19800 {
		t.Fatalf("counts %d %d", len(d.Keys), d.Count(compare.Different))
	}
}

func TestCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := compare.Environments(ctx, env("l", map[string]string{"A": "1"}), env("r", nil))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}
