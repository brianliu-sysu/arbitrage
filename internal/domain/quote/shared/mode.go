package shared

// QuoteMode selects exact-input or exact-output quoting.
type QuoteMode int

const (
	QuoteModeExactInput QuoteMode = iota + 1
	QuoteModeExactOutput
)

func (m QuoteMode) IsExactInput() bool {
	return m == QuoteModeExactInput
}

func (m QuoteMode) IsExactOutput() bool {
	return m == QuoteModeExactOutput
}
