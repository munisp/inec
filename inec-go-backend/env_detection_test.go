package main

import "testing"

// TestIsProductionHonorsBothEnvVars proves production detection cannot be
// bypassed by either naming convention: APP_ENV or INEC_ENV alone must
// trigger the fail-closed production path (mirrors gotv.IsProductionEnv).
func TestIsProductionHonorsBothEnvVars(t *testing.T) {
	cases := []struct {
		name         string
		appEnv       string
		inecEnv      string
		wantProd     bool
		wantProdLike bool
	}{
		{"both unset", "", "", false, false},
		{"app production", "production", "", true, true},
		{"inec production", "", "production", true, true},
		{"app staging only", "staging", "", false, true},
		{"inec staging only", "", "staging", false, true},
		{"development", "development", "development", false, false},
		{"test", "test", "", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("APP_ENV", tc.appEnv)
			t.Setenv("INEC_ENV", tc.inecEnv)
			if got := isProduction(); got != tc.wantProd {
				t.Errorf("isProduction() = %v, want %v", got, tc.wantProd)
			}
			if got := isProductionLike(); got != tc.wantProdLike {
				t.Errorf("isProductionLike() = %v, want %v", got, tc.wantProdLike)
			}
		})
	}
}
