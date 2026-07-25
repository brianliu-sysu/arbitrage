package quotepancakev3

import quoteappclv3 "github.com/brianliu-sysu/uniswapv3/internal/application/quote/clv3"

type (
	Request  = quoteappclv3.Request
	Response = quoteappclv3.Response
)

// AppService orchestrates PancakeSwap V3 route discovery and quoting.
type AppService struct {
	*quoteappclv3.AppService
}
