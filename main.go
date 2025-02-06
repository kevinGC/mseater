package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"math"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/kevinGC/mseater/crawler"
)

// TODO: More search parameters: custom "margin" of seats (instead of default 3)
// TODO: More search parameters: custom U/D/L/R "margin" of seats
// TODO: More search parameters: seats with no neighbors

const dateLayout = "01-02"

var (
	zipRegex   = regexp.MustCompile(`^[0-9]{5}$`)
	rangeRegex = regexp.MustCompile(`^([0-9]+)-([0-9]+)$`)
)

func main() {
	// Only Exit(1) here to avoid accidentally skipping defers.
	if err := run(); err != nil {
		fmt.Printf("Failure: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Parse flags.
	var (
		// Search parameters.
		title    string
		date     date
		zip      zip
		numSeats int

		// Output controls.
		link    bool
		showBad bool

		// Request controls.
		timeout         time.Duration
		retry           bool
		requestInterval durationRange

		// Debug controls.
		debug        bool
		debugStep    debugStepArg
		showingLimit uint
	)

	// Defaults.
	date.Set("today")
	requestInterval.Set("15-25")

	flag.StringVar(&title, "title", "", "All or part of the movie title.")
	flag.Var(&date, "date", `Day to search as MM-DD or "today", "tomorrow", or a weekday e.g. "tuesday".`)
	flag.Var(&zip, "zip", "Zip code to search near.")
	flag.IntVar(&numSeats, "num-seats", 2, "The number of contiguous seats to find.")

	flag.BoolVar(&link, "link", false, "Whether to show links in showtime results.")
	flag.BoolVar(&showBad, "show-bad", false, "Whether to also output bad showtimes.")

	flag.DurationVar(&timeout, "timeout", 0 /* unlimited */, "The timeout for searching.")
	flag.BoolVar(&retry, "retry", true, "Whether to retry failed seat crawling.")
	flag.Var(&requestInterval, "request-interval", "The interval, in seconds, between making HTTP requests. This can be "+
		"either a number (e.g. \"5\") or a range (e.g. \"3-10\"). This helps avoid being flagged as a bot by websites (and you're "+
		"not a bot! You want to see the information they have on their site!).")

	flag.BoolVar(&debug, "debug", false, "Whether to show debug log output.")
	flag.Var(&debugStep, "debug-step", "Which step to debug and its relevant arguments, which depends on the particular step.")
	flag.UintVar(&showingLimit, "showing-limit", math.MaxUint, "The max number of showings to check. Negative means unlimited.")

	flag.Parse()

	// Flag error checking.
	if title == "" {
		return fmt.Errorf("no title provded (use --title)")
	}

	if numSeats < 1 {
		return fmt.Errorf("too few seats specified: must be at least 1")
	}

	if zip.zip == "" {
		return fmt.Errorf("no zip code provided (use --zip)")
	}

	if debug {
		handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{})
		slog.SetDefault(slog.New(NewLevelHandler(slog.LevelDebug, handler)))
	}

	// Cancellations via context.
	ctx := context.Background()
	cancel := func() {}
	if timeout != 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	// Construct the request.
	req := crawler.Request{
		Title:           title,
		Date:            date.date,
		Zip:             zip.zip,
		NumSeats:        numSeats,
		ShowingLimit:    showingLimit,
		Retry:           retry,
		RequestInterval: requestInterval.DurationRange,
	}

	// When set, perform only the step requested by the user instead of the
	// full search.
	switch debugStep.step {
	case stepNone:
	case stepSearch:
		result, err := crawler.CrawlSearch(ctx, req)
		log.Printf("crawler.CrawlSearch(%+v) returned error: %v)", req, err)
		fmt.Printf("%s\n", formatShowings(result.Showings, link))
		return nil
	case stepSeats:
		ok, err := crawler.CrawlSeats(ctx, req, debugStep.link)
		log.Printf("crawler.CrawlSeats(%+v, %s) returned (%t, %v)", req, debugStep.link, ok, err)
		return nil
	default:
		panic(fmt.Sprintf("unknown debugStep: %d", debugStep.step))
	}

	// Perform the search.
	result, err := crawler.Crawl(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to get showtimes: %v", err)
	}

	// Print results.
	slices.SortFunc(result.Showings, func(a, b crawler.Showing) int { return a.Compare(b) })
	fmt.Printf("%s\n", formatShowings(result.Showings, link))
	if showBad {
		fmt.Printf("=== Bad showings ===\n")
		fmt.Printf("%s\n", formatShowings(result.BadShowings, link))
	}

	return nil
}

func formatShowings(showings []crawler.Showing, printLinks bool) string {
	var builder strings.Builder
	writer := tabwriter.NewWriter(&builder, 0, 0, 1, ' ', 0)
	for _, showing := range showings {
		fmt.Fprintf(writer, "%s\t%s", showing.Theater, showing.When.Format("3:04pm"))
		if printLinks {
			fmt.Fprintf(writer, "\t%s", showing.Link)
		}
		fmt.Fprintf(writer, "\n")
	}
	writer.Flush()
	return builder.String()
}

type date struct {
	date time.Time
}

func (dt *date) String() string {
	return dt.date.Format(dateLayout)
}

func (dt *date) Set(input string) error {
	switch day := strings.ToLower(input); day {
	case "tomorrow":
		dt.date = time.Now().AddDate(0 /* years */, 0 /* months */, 1 /* day */)
		return nil
	case "today":
		dt.date = time.Now()
		return nil
	case "monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday":
		dt.date = time.Now().AddDate(0 /* years */, 0 /* months */, 1 /* day */)
		for {
			if strings.ToLower(dt.date.Weekday().String()) == day {
				return nil
			}
			dt.date = time.Now().AddDate(0 /* years */, 0 /* months */, 1 /* day */)
		}
	}

	in, err := time.Parse(dateLayout, input)
	if err != nil {
		return err
	}
	dt.date = in

	// Set year to whenever this date occurs next.
	now := time.Now()
	dt.date.AddDate(now.Year(), 0 /* months */, 0 /* days */)
	if dt.date.Before(now) {
		dt.date.AddDate(1, 0 /* months */, 0 /* days */)
	}

	return nil
}

type zip struct {
	zip string
}

func (zp *zip) String() string {
	return zp.zip
}

func (zp *zip) Set(input string) error {
	if !zipRegex.MatchString(input) {
		return fmt.Errorf("%q is not a valid 5 digit zip code", input)
	}
	zp.zip = input
	return nil
}

type debugStep int

const (
	stepNone debugStep = iota
	stepSearch
	stepSeats
)

type debugStepArg struct {
	step debugStep

	// link is used by stepSeats
	link string
}

func (ds *debugStepArg) String() string {
	switch ds.step {
	case stepNone:
		return ""
	case stepSearch:
		return "search"
	case stepSeats:
		return fmt.Sprintf("seats:%s", ds.link)
	default:
		panic(fmt.Sprintf("unknown debug step %d", ds.step))
	}
}

func (ds *debugStepArg) Set(input string) error {
	switch {
	case input == "":
		ds.step = stepNone
	case input == "search":
		ds.step = stepSearch
	case strings.HasPrefix(input, "seats:"):
		ds.step = stepSeats
		ds.link, _ = strings.CutPrefix(input, "seats:")
	default:
		return fmt.Errorf("unknown step: %s", input)
	}
	return nil
}

type durationRange struct {
	crawler.DurationRange
}

func (dr *durationRange) String() string {
	if dr.Lower == dr.Upper {
		return fmt.Sprintf("%s", dr.Lower)
	}
	return fmt.Sprintf("%s-%s", dr.Lower, dr.Upper)
}

func (dr *durationRange) Set(input string) error {
	if val, err := strconv.Atoi(input); err == nil {
		val := time.Duration(val) * time.Second
		dr.Upper = val
		dr.Lower = val
		return nil
	}
	matches := rangeRegex.FindStringSubmatch(input)
	if len(matches) != 3 {
		return fmt.Errorf("invalid interval: %q", input)
	}
	lower, err := strconv.Atoi(matches[1])
	if err != nil {
		return fmt.Errorf("invalid interval lower bound %q: %w", matches[1], err)
	}
	upper, err := strconv.Atoi(matches[2])
	if err != nil {
		return fmt.Errorf("invalid interval upper bound %q: %w", matches[2], err)
	}
	if upper < lower {
		return fmt.Errorf("upper bound %q cannot be greater than lower bound %q", upper, lower)
	}
	dr.Upper = time.Duration(upper) * time.Second
	dr.Lower = time.Duration(lower) * time.Second
	return nil
}
