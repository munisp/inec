package main

import "testing"

func TestShouldSeedE2EFixtures(t *testing.T) {
	tests := []struct {
		name          string
		appEnv        string
		githubActions string
		explicitSeed  string
		want          bool
	}{
		{name: "test environment", appEnv: "test", want: true},
		{name: "e2e environment", appEnv: "e2e", want: true},
		{name: "explicit non-production seed", appEnv: "development", explicitSeed: "true", want: true},
		{name: "github actions", appEnv: "", githubActions: "true", want: true},
		{name: "production blocks explicit seed", appEnv: "production", explicitSeed: "true", want: false},
		{name: "staging blocks github actions", appEnv: "staging", githubActions: "true", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("APP_ENV", tt.appEnv)
			t.Setenv("GITHUB_ACTIONS", tt.githubActions)
			t.Setenv("INEC_E2E_SEED", tt.explicitSeed)
			if got := shouldSeedE2EFixtures(); got != tt.want {
				t.Fatalf("shouldSeedE2EFixtures() = %t, want %t", got, tt.want)
			}
		})
	}
}
