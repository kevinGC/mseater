// Package crawler does the actual crawling work to get seat maps.
package crawler

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	playwright "github.com/playwright-community/playwright-go"
	"golang.org/x/exp/rand"
)

// TODO: What about those weird sponsor sections? Do those work?
// TODO: Handle theaters that don't have info, as currently they hang.
// TODO: Try something besides playwright. A native Go library might work better.

const (
	retries   = 3
	userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"
)

type Request struct {
	// Title is a substring of the movie title.
	Title string
	// Date is the day to search showings for.
	Date time.Time
	// Zip is the zip code to search near.
	Zip string
	// NumSeats is the number of seats to reserve.
	NumSeats int

	// ShowingLimit limits the number of showings to check. Useful for
	// debugging.
	ShowingLimit uint
	// Retry is whether to retry seat crawling. The seating page is fairly slow
	// to load and in some cases fails to render altogether.
	Retry bool
	// DurationRange is range of time to wait between HTTP requests.
	RequestInterval DurationRange
}

// DurationRange is a range of allowable durations.
type DurationRange struct {
	// Lower is the lower bound on a duration.
	Lower time.Duration
	// Lower is the upper bound on a duration.
	Upper time.Duration
}

// Random returns a random duration within the range.
func (dr *DurationRange) Random() time.Duration {
	if dr.Upper == dr.Lower {
		return dr.Upper
	}
	return time.Duration(rand.Int63n(int64(dr.Upper-dr.Lower))) + dr.Lower
}

// A Result holds the output of a search.
type Result struct {
	Showings    []Showing
	BadShowings []Showing
}

// A Showing is a single screening of a movie.
type Showing struct {
	Link    string
	Theater string
	When    time.Time

	// Not really part of the api -- consider splitting out.
	Retries int
}

// Compare returns -1/0/1 depending on the relative ordering of sh and other.
func (sh *Showing) Compare(other Showing) int {
	if cmp := strings.Compare(sh.Theater, other.Theater); cmp != 0 {
		return cmp
	}
	return sh.When.Compare(other.When)
}

// Crawl performs a full search based on req.
func Crawl(ctx context.Context, req Request) (Result, error) {
	return crawlSearch(ctx, req, false /* skipCrawlSeats */)
}

// CrawlSearch returns only the showing times and locations for req. It does not crawl seats.
func CrawlSearch(ctx context.Context, req Request) (Result, error) {
	return crawlSearch(ctx, req, true /* skipCrawlSeats */)
}

func crawlSearch(ctx context.Context, req Request, skipCrawlSeats bool) (Result, error) {
	// Startup a browser.
	browser, cleanup, err := startBrowser()
	if err != nil {
		return Result{}, fmt.Errorf("failed to start browser: %w", err)
	}
	defer cleanup()

	stop := context.AfterFunc(ctx, func() { _ = browser.Close })
	defer stop()

	// Get the showings.
	res, err := showings(req, browser)
	if err != nil {
		return Result{}, fmt.Errorf("failed to get showings: %w", err)
	}
	slog.Debug("finished parsing showings", "numShowings", len(res.Showings))

	if skipCrawlSeats {
		return res, nil
	}

	// Inspect the seating.
	var (
		good     []Showing
		failures []Showing
		nCrawled int
	)
	for ; nCrawled < len(res.Showings); nCrawled++ { // TODO: bad way to pass showings back up, inside the response struct
		if uint(nCrawled) >= req.ShowingLimit {
			break
		}
		showing := &res.Showings[nCrawled]
		ok, err := crawlSeats(req, browser, showing.Link)
		if err != nil {
			showing.Retries++
			slog.Info("failed to check seats", " page", showing.Link, "retries", showing.Retries, "err", err)
			if req.Retry && showing.Retries < retries {
				nCrawled--
			} else {
				failures = append(failures, *showing)
			}
			continue
		}
		if ok {
			good = append(good, *showing)
		} else {
			res.BadShowings = append(res.BadShowings, *showing)
		}
	}
	slog.Debug("seat crawlers finished", "goodShowings", len(good))
	fmt.Printf("Failed %d of %d requests (%f%% failures rate)\n", len(failures), nCrawled, float32(len(failures))/float32(nCrawled))
	fmt.Printf("Failed to handle the following URLs. You may want to check them yourself (or even file a bug report!):\n")
	for _, showing := range failures {
		fmt.Printf("\t%s\n", showing.Link)
	}

	res.Showings = good
	return res, nil
}

func showings(req Request, browser playwright.Browser) (Result, error) {
	browserCtx, err := browser.NewContext(playwright.BrowserNewContextOptions{UserAgent: playwright.String(userAgent)})
	if err != nil {
		return Result{}, fmt.Errorf("failed to create context: %w", err)
	}
	defer browserCtx.Close()

	pg, err := browserCtx.NewPage()
	if err != nil {
		return Result{}, fmt.Errorf("failed to create page: %w", err)
	}
	defer pg.Close()
	page := rateLimitedPage{Page: pg, interval: req.RequestInterval}

	// Navigate to the search page and get a list of theaters.
	searchURL := fmt.Sprintf("https://www.fandango.com/%s_movietimes?date=%s", req.Zip, req.Date.Format("2006-01-02"))
	slog.Debug("searching", "URL", searchURL)
	if _, err := page.Goto(searchURL); err != nil {
		return Result{}, fmt.Errorf("failed to load page at %q: %w", searchURL, err)
	}
	theaters, err := page.Locator(".fd-showtimes .fd-theater").All()
	if err != nil || len(theaters) == 0 {
		return Result{}, fmt.Errorf("failed to find theaters on page %q: %w", searchURL, err)
	}

	// From here on out, errors aren't fatal. That is: we can fail with one
	// theater or showing, but succeed with another. So errors are logged, not
	// returned.

	// slog-friendly {k, v, k, v, ...} context for errors.
	errCtx := []any{"searchPage", searchURL}

	var res Result
	for _, theater := range theaters {
		// Every iteration gets its own shadow of errCtx. We add elements as we
		// go, and those elements propogate down the call stack. But the next
		// iteration gets only the relevant elements from outside the loop.
		errCtx := errCtx

		// Get the name of the theater.
		theaterNameNodes, err := theater.Locator(".fd-theater__name > a").All()
		if err != nil || len(theaterNameNodes) == 0 {
			info("failed to find theater name nodes", errCtx, "err", err, "ntheaternodes", len(theaterNameNodes))
			continue
		}
		theaterName, err := theaterNameNodes[0].TextContent()
		if err != nil {
			info("failed to get text content of theater name node", errCtx, "err", err)
			continue
		}
		theaterName = strings.TrimSpace(theaterName)
		errCtx = append(errCtx, "theater", theaterName)
		slog.Debug("handling theater", "theaterName", theaterName)

		// Iterate over the movies at this theater.
		movieNodes, err := theater.Locator(".fd-movie").All()
		if err != nil || len(movieNodes) == 0 {
			info("failed to find a movie node on page", errCtx, "err", err, "nmovienodes", len(movieNodes))
			continue
		}

		for _, movieNode := range movieNodes {
			errCtx := errCtx

			// Find a movie that matches the title. Some theaters report no
			// showings, which we catch here.
			noShowtimeLocator := movieNode.Locator(".fd-movie__no-showtimes")
			titleLocator := movieNode.Locator(".fd-movie__title")
			titleOrNoShowtimeNode := noShowtimeLocator.Or(titleLocator).First()
			noShowtimes, err := noShowtimeLocator.IsVisible()
			if err != nil {
				info("failed to check visiblity of no showtime locator", errCtx, "err", err)
				continue
			}
			if noShowtimes {
				slog.Debug("no showings available", errCtx...)
				continue
			}

			titleNode := titleOrNoShowtimeNode
			if titleNode == nil { // TODO: Some of these len checks can be removed, and we can just First() instead of all.
				info("failed to find a movie title for", errCtx, "err", err)
				continue
			}
			var timeoutMS float64 = 30_000 // TODO: Find a better way to check for this.
			title, err := titleNode.TextContent(playwright.LocatorTextContentOptions{Timeout: &timeoutMS})
			if err != nil {
				info("failed to get text content of title node", errCtx, "err", err)
				continue
			}
			if !strings.Contains(strings.ToLower(title), strings.ToLower(req.Title)) {
				continue
			}
			slog.Debug("found matching movie", "title", title)
			errCtx = append(errCtx, "title", title)

			// Find variants with reserved seating.
			variants, err := movieNode.Locator("li.fd-movie__showtimes-variant").All()
			if err != nil || len(variants) == 0 {
				info("failed when finding variants", errCtx, "nvariants", len(variants))
				continue
			}

			for i, variant := range variants {
				errCtx := errCtx

				slog.Debug("checking variant", "variant", i)
				// Only get showtimes with reserved seating.
				amenities, err := variant.Locator(".fd-movie__amenity-list > li > button").All()
				if err != nil {
					info("failed to get amenities list", errCtx, "err", err)
					continue
				}
				var reserved bool
				for _, amenity := range amenities {
					text, err := amenity.TextContent()
					if err != nil {
						info("failed to get text content for amenity", errCtx, "err", err)
						continue
					}
					if strings.Contains(strings.ToLower(text), "reserve") {
						reserved = true
						slog.Debug("found reserved seating", "amenity", text)
						break
					}
				}
				if !reserved {
					continue
				}

				// Get showings.
				showings, err := variant.Locator("li.showtimes-btn-list__item > a").All()
				if err != nil || len(showings) == 0 {
					info("failed to get showings list", errCtx, "err", err, "nshowings", len(showings))
					continue
				}
				slog.Debug("found showings", "nshowings", len(showings))
				for _, showing := range showings {
					errCtx := errCtx

					text, err := showing.TextContent()
					if err != nil {
						info("failed to get text content for showing", errCtx, "err", err)
						continue
					}
					slog.Debug("found showing", "time", text)

					// The text is a bunch of whitespace
					// surrounding a string like "9:30a" or
					// "12:30p".
					showtime, err := time.Parse("3:04pm", strings.TrimSpace(text)+"m")
					if err != nil {
						info("failed to parse time", errCtx, "err", err, "time", text)
						continue
					}
					showtime = showtime.AddDate(
						req.Date.Year(),
						int(req.Date.Month()),
						req.Date.Day(),
					)
					errCtx = append(errCtx, "showtime", showtime)

					link, err := showing.GetAttribute("href")
					if err != nil {
						info("failed to get link", errCtx, "err", err)
						continue
					}
					errCtx = append(errCtx, "seatsLink", link)

					res.Showings = append(res.Showings, Showing{
						Link:    link,
						Theater: theaterName,
						When:    showtime,
					})
				}
			}
		}
	}

	return res, nil
}

type seat struct {
	row      int
	col      int
	reserved bool
}

// TODO: Sometimes we get directed to a page where we choose between "classes"
// of seats. We'll have to handle those.
// TODO: Get smarter about determining seat location and what counts as good.
func CrawlSeats(ctx context.Context, req Request, link string) (bool, error) {
	browser, cleanup, err := startBrowser()
	if err != nil {
		return false, fmt.Errorf("failed to start browser: %w", err)
	}
	defer cleanup()

	stop := context.AfterFunc(ctx, func() { _ = browser.Close })
	defer stop()

	// This is a one-off. Ignore the interval.
	return crawlSeats(req, browser, link)
}

func crawlSeats(req Request, browser playwright.Browser, link string) (bool, error) {
	slog.Debug("crawling seats", "URL", link)
	// Navigate to the search page and get a list of theaters.
	browserCtx, err := browser.NewContext(playwright.BrowserNewContextOptions{UserAgent: playwright.String(userAgent)})
	if err != nil {
		return false, fmt.Errorf("failed to create context: %w", err)
	}
	defer browserCtx.Close()

	pg, err := browserCtx.NewPage()
	if err != nil {
		return false, fmt.Errorf("failed to create seat page: %w", err)
	}
	defer pg.Close()
	page := rateLimitedPage{Page: pg, interval: req.RequestInterval}

	if _, err := page.Goto(link); err != nil {
		return false, fmt.Errorf("failed to load page at %q: %w", link, err)
	}

	// We have to parse the seating chart. We make the following
	// assumptions based on poking around some pages:
	//
	//   - The seating chart is just a giant list of divs.
	//   - Seats are listed left to right, top to bottom.
	//   - Seats are all absolutely positioned.
	//   - Seats in a row all have the same `height:` property.
	//
	// So we iterate over the list of seats and infer that a new row starts
	// whenever the height changes. We also assume that rows are centered,
	// which isn't totally true: rows are often missing a few seats at one
	// end. But it should be good enough for now.

	// TODO: Play with this timeout.
	var seatMapTimeoutMS float64 = 30_000
	if err := page.Locator(".seat-map__seat").First().WaitFor(playwright.LocatorWaitForOptions{Timeout: &seatMapTimeoutMS}); err != nil {
		return false, fmt.Errorf("failed to wait for seats on page: %v", err)
	}

	seatDivs, err := page.Locator(".seat-map__seat:not(.wheelchair):not(.companion)").All()
	if err != nil {
		return false, fmt.Errorf("failed to find seats: %w", err)
	} else if len(seatDivs) == 0 {
		str, err := page.Content()
		if err != nil {
			panic(err)
		}
		tmp, err := os.CreateTemp("", "seating-")
		if err != nil {
			panic(err)
		}
		if _, err := fmt.Fprint(tmp, str); err != nil {
			panic(err)
		}
		slog.Info("no seats found", "URL", page.URL(), "pageDump", tmp.Name())

		return false, fmt.Errorf("no seats found with link: %q", link)
	}

	// Currently, building the seat map and checking for good seats
	// are separate. We could save time by doing these at the same time, but
	// this is so computationally inexpensive that it's not worth the
	// complexity.

	// Generate a list of seats and get the number of rows and columns.
	var (
		curTop string
		col    int
		maxCol int
		row    = -1
		// Most theaters have fewer than 256 seats.
		seats = make([]seat, 0, 256)
	)
	for _, seatDiv := range seatDivs {
		// Skip handicap and companion seats.
		// TODO: Support these as an option, but for now we can't just
		// say "every show has available seats" because handicap seats
		// are open.

		// Update when we hit a new row.
		handle, err := seatDiv.Evaluate(
			"element => window.getComputedStyle(element).getPropertyValue('top')",
			nil,
		)
		if err != nil {
			return false, fmt.Errorf("failed to get seat element top: %w", err)
		}
		top := handle.(string)
		if top != curTop {
			curTop = top
			row++
			col = 0
		}

		disabled, err := seatDiv.GetAttribute("aria-disabled")
		if err != nil {
			return false, fmt.Errorf("failed to get reservation status: %w", err)
		}
		var reserved bool
		switch disabled {
		case "true":
			reserved = true
		case "false":
		default:
			return false, fmt.Errorf("failed to parse aria-disabled attribute %q", disabled)
		}
		seats = append(seats, seat{row: row, col: col, reserved: reserved})

		maxCol = max(maxCol, col)
		col++
	}

	good := checkSeats(seats, row, maxCol, req.NumSeats)
	slog.Debug("crawled seats", "URL", link, "good", true)
	return good, nil
}

func checkSeats(seats []seat, maxRow, maxCol, numSeats int) bool {
	// Look for suitable seats. Currently, we look for N adjacent seats in
	// the same row that aren't within 3 seats of an edge. An edge is
	// defined by row and column zero along with the max row and column.
	// Again this is fucky for rows of different length, but for now:
	// ¯\_(ツ)_/¯.
	const buffer = 3
	var contiguous int
	for _, seat := range seats {
		// Did we enter a new row?
		if seat.col == 0 {
			contiguous = 0
		}
		// Break out early if we're in the back rows. Since these are
		// ordered left to right, front to back, once we reach the back
		// we know there're no more good seats.
		if seat.row > maxRow-buffer {
			break
		}
		if seat.row < buffer || seat.col < buffer || seat.col > maxCol-buffer || seat.reserved {
			contiguous = 0
			continue
		}

		contiguous++
		if contiguous >= numSeats {
			return true
		}
	}

	return false
}

// startBrowser returns a Browser, cleanup method, and error.
func startBrowser() (playwright.Browser, func(), error) {
	// Boot up playwright.
	opts := &playwright.RunOptions{SkipInstallBrowsers: true}
	if err := playwright.Install(opts); err != nil {
		return nil, nil, fmt.Errorf("failed to install playwright drivers: %w", err)
	}
	pw, err := playwright.Run()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to run playwright: %w", err)
	}
	browser, err := pw.Chromium.Launch()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to launch browser (do you have NPM and NPX installed?): %w, pw.Stop(): %v", err, pw.Stop())
	}
	cleanup := func() {
		if err := browser.Close(); err != nil {
			slog.Info("failed to stop browser", "err", err)
		}
		if err := pw.Stop(); err != nil {
			slog.Info("failed to stop playwright", "err", err)
		}
	}
	return browser, cleanup, nil
}

func info(msg string, errCtx []any, args ...any) {
	slog.Info(msg, append(errCtx, args)...)
}
