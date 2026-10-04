package main

import (
	"context"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"

	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/foundry"
)

// tokenSource returns the credential used for the Foundry data plane. XFOUNDRY_ACCESS_TOKEN
// supplies a ready-made token (for tests and pipelines that already hold one); otherwise the
// default Azure credential chain is used (environment, workload identity, managed identity,
// Azure CLI, Azure Developer CLI).
func tokenSource() (foundry.TokenFunc, error) {
	if t := os.Getenv("XFOUNDRY_ACCESS_TOKEN"); t != "" {
		return func(context.Context) (string, error) { return t, nil }, nil
	}
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) (string, error) {
		tok, err := cred.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{foundry.Scope}})
		if err != nil {
			return "", err
		}
		return tok.Token, nil
	}, nil
}
