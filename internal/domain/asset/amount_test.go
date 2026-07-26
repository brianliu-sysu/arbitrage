package asset

import "testing"

func TestParseDecimalAmountUsesTokenDecimals(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		decimals uint8
		want     string
	}{
		{name: "USDC", value: "1.25", decimals: 6, want: "1250000"},
		{name: "WETH", value: "0.1", decimals: 18, want: "100000000000000000"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseDecimalAmount(test.value, test.decimals)
			if err != nil {
				t.Fatalf("parse amount: %v", err)
			}
			if got.String() != test.want {
				t.Fatalf("expected %s, got %s", test.want, got)
			}
		})
	}
}

func TestParseDecimalAmountRejectsExcessPrecision(t *testing.T) {
	if _, err := ParseDecimalAmount("0.0000001", 6); err == nil {
		t.Fatal("expected excess precision error")
	}
}
