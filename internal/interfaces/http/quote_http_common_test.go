package httpapi

import (
	"testing"
)

func TestParseQuoteBaseAllowNativeETH(t *testing.T) {
	t.Parallel()

	_, _, _, _, _, err := parseQuoteBaseAllowNative(quoteHTTPRequest{
		TokenIn:  "0x514910771AF9Ca656af840dff83E8264EcF986CA",
		TokenOut: "0x0000000000000000000000000000000000000000",
		AmountIn: "1",
	}, true)
	if err != nil {
		t.Fatalf("expected native ETH tokenOut to be accepted, got %v", err)
	}
}

func TestParseQuoteBaseRejectsNativeETHForV3(t *testing.T) {
	t.Parallel()

	_, _, _, _, _, err := parseQuoteBase(quoteHTTPRequest{
		TokenIn:  "0x514910771AF9Ca656af840dff83E8264EcF986CA",
		TokenOut: "0x0000000000000000000000000000000000000000",
		AmountIn: "1",
	})
	if err == nil {
		t.Fatal("expected native ETH tokenOut to be rejected for v3-style parsing")
	}
}
