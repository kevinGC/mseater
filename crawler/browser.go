package crawler

import (
	"fmt"
	"time"

	playwright "github.com/playwright-community/playwright-go"
)

// Pages are rate limited globally.
var started bool

type rateLimitedPage struct {
	playwright.Page
	interval DurationRange
}

func (rlp *rateLimitedPage) Goto(url string, options ...playwright.PageGotoOptions) (playwright.Response, error) {
	if started {
		time.Sleep(rlp.interval.Random())
	} else {
		started = true
	}
	fmt.Printf("Visiting %s\n", url)
	return rlp.Page.Goto(url, options...)
}
