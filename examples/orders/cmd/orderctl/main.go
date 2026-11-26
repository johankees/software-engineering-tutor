// Command orderctl is the command-line client for the orders service.
//
// Usage:
//
//	orderctl [global flags] <command> [command flags]
//
// Run "orderctl -h" for the commands and flags.
package main

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"
	"uuid"

	"github.com/acme/orders/internal/buildinfo"
	"github.com/acme/orders/internal/client"
	"github.com/acme/orders/internal/order"
)

// Exit codes. Scripts depend on them, so they are part of the CLI's API.
const (
	exitOK    = 0
	exitError = 1 // the command ran and failed
	exitUsage = 2 // the command line was wrong (same code the flag package uses)
	// exitInterrupted is 128 + SIGINT(2), what shells report for Ctrl-C.
	exitInterrupted = 130
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr)
	// Check before stop(): stop also cancels ctx.
	if err != nil && ctx.Err() != nil {
		fmt.Fprintln(os.Stderr, "orderctl: interrupted")
		os.Exit(exitInterrupted)
	}
	stop()
	if code := exitCode(err); code != exitOK {
		if !alreadyReported(err) {
			fmt.Fprintln(os.Stderr, "orderctl:", err)
		}
		os.Exit(code)
	}
}

// usageError marks mistakes in the command line, so main can exit with 2.
// reported is true when the flag package already printed the problem.
type usageError struct {
	err      error
	reported bool
}

func (e usageError) Error() string { return e.err.Error() }
func (e usageError) Unwrap() error { return e.err }

func usagef(format string, args ...any) error {
	return usageError{err: fmt.Errorf(format, args...)}
}

func exitCode(err error) int {
	if err == nil || errors.Is(err, flag.ErrHelp) {
		return exitOK
	}
	if _, ok := errors.AsType[usageError](err); ok {
		return exitUsage
	}
	return exitError
}

func alreadyReported(err error) bool {
	if errors.Is(err, flag.ErrHelp) {
		return true
	}
	ue, ok := errors.AsType[usageError](err)
	return ok && ue.reported
}

const usageText = `Usage: orderctl [global flags] <command> [command flags]

Commands:
  create   create an order: create -customer ID -item SKU:QTY:PRICE_CENTS [-item ...]
  get      show one order: get ORDER_ID
  list     list all orders
  version  print version information

Global flags:
`

// globals are the flags shared by every command.
type globals struct {
	server  string
	timeout time.Duration
	output  string
}

// run is the real main: all inputs and outputs are parameters, so tests can
// drive the CLI completely in-process.
func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) error {
	var g globals
	fs := flag.NewFlagSet("orderctl", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), usageText)
		fs.PrintDefaults()
	}
	fs.StringVar(&g.server, "server", cmp.Or(getenv("ORDERS_SERVER"), "http://localhost:8080"), "orders service `URL` (env ORDERS_SERVER)")
	fs.DurationVar(&g.timeout, "timeout", 10*time.Second, "timeout for the whole command")
	fs.StringVar(&g.output, "o", "table", "output `format`: table or json")
	if err := fs.Parse(args); err != nil {
		return wrapFlagErr(err)
	}
	if g.output != "table" && g.output != "json" {
		return usagef("-o %q: must be table or json", g.output)
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return usagef("no command given")
	}

	cmd, cmdArgs := fs.Arg(0), fs.Args()[1:]
	if cmd == "version" {
		_, err := fmt.Fprintln(stdout, "orderctl", buildinfo.Read())
		return err
	}

	// One deadline for the whole command, and Ctrl-C cancels it too.
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	// No per-request timeout on the http.Client: the context above already
	// bounds the whole command, and two competing timeouts confuse users.
	c, err := client.New(g.server, "orderctl/"+buildinfo.Read().Version, &http.Client{})
	if err != nil {
		return usagef("-server: %w", err)
	}
	out := printer{w: stdout, format: g.output}

	switch cmd {
	case "create":
		return runCreate(ctx, c, out, cmdArgs, stderr)
	case "get":
		return runGet(ctx, c, out, cmdArgs)
	case "list":
		return runList(ctx, c, out, cmdArgs)
	default:
		return usagef("unknown command %q (run orderctl -h)", cmd)
	}
}

func runCreate(ctx context.Context, c *client.Client, out printer, args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("orderctl create", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		customer string
		items    itemList
	)
	fs.StringVar(&customer, "customer", "", "customer `ID` (required)")
	fs.Var(&items, "item", "order line as `SKU:QTY:PRICE_CENTS`; repeat for more lines")
	if err := fs.Parse(args); err != nil {
		return wrapFlagErr(err)
	}
	if fs.NArg() > 0 {
		return usagef("create: unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	o, err := c.CreateOrder(ctx, order.NewOrder{CustomerID: customer, Items: items})
	if err != nil {
		return fmt.Errorf("create order: %w", err)
	}
	return out.orders(o)
}

func runGet(ctx context.Context, c *client.Client, out printer, args []string) error {
	if len(args) != 1 {
		return usagef("get: want exactly one ORDER_ID, got %d arguments", len(args))
	}
	id, err := uuid.Parse(args[0])
	if err != nil {
		return usagef("get: %q is not a valid order ID", args[0])
	}
	o, err := c.GetOrder(ctx, id)
	if err != nil {
		return fmt.Errorf("get order: %w", err)
	}
	return out.orders(o)
}

func runList(ctx context.Context, c *client.Client, out printer, args []string) error {
	if len(args) != 0 {
		return usagef("list: takes no arguments")
	}
	orders, err := c.ListOrders(ctx)
	if err != nil {
		return fmt.Errorf("list orders: %w", err)
	}
	return out.orders(orders...)
}

// itemList is a repeatable -item flag. Any type with String and Set
// methods satisfies flag.Value.
type itemList []order.Item

func (l *itemList) String() string { return fmt.Sprint(len(*l), " items") }

func (l *itemList) Set(s string) error {
	sku, rest, ok := strings.Cut(s, ":")
	qty, price, ok2 := strings.Cut(rest, ":")
	if !ok || !ok2 {
		return fmt.Errorf("want SKU:QTY:PRICE_CENTS, got %q", s)
	}
	q, err := strconv.Atoi(qty)
	if err != nil {
		return fmt.Errorf("quantity %q: not a whole number", qty)
	}
	p, err := strconv.ParseInt(price, 10, 64)
	if err != nil {
		return fmt.Errorf("price %q: not a whole number of cents", price)
	}
	*l = append(*l, order.Item{SKU: sku, Quantity: q, PriceCents: p})
	return nil
}

// printer writes results to stdout: a table for humans, JSON for scripts.
// Messages and errors go to stderr, so "orderctl list -o json | jq" works.
type printer struct {
	w      io.Writer
	format string
}

// orderOutput is the CLI's JSON output contract. Scripts parse it, so it is
// defined here and changes only on purpose.
type orderOutput struct {
	ID         string    `json:"id"`
	CustomerID string    `json:"customer_id"`
	Status     string    `json:"status"`
	Items      int       `json:"item_count"`
	TotalCents int64     `json:"total_cents"`
	CreatedAt  time.Time `json:"created_at"`
}

func (p printer) orders(orders ...order.Order) error {
	if p.format == "json" {
		out := make([]orderOutput, len(orders))
		for i, o := range orders {
			out[i] = orderOutput{
				ID: o.ID.String(), CustomerID: o.CustomerID, Status: string(o.Status),
				Items: len(o.Items), TotalCents: o.TotalCents(), CreatedAt: o.CreatedAt,
			}
		}
		b, err := json.Marshal(out, jsontext.WithIndent("  "))
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(p.w, "%s\n", b)
		return err
	}

	tw := tabwriter.NewWriter(p.w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tCUSTOMER\tSTATUS\tITEMS\tTOTAL\tCREATED")
	for _, o := range orders {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\n",
			o.ID, o.CustomerID, o.Status, len(o.Items), formatCents(o.TotalCents()),
			o.CreatedAt.Format(time.DateTime))
	}
	return tw.Flush()
}

func formatCents(c int64) string {
	sign := ""
	if c < 0 {
		sign, c = "-", -c
	}
	return fmt.Sprintf("%s%d.%02d", sign, c/100, c%100)
}

// wrapFlagErr keeps flag.ErrHelp as is (exit 0) and marks every other
// flag error as a usage error (exit 2). The flag package already printed it.
func wrapFlagErr(err error) error {
	if errors.Is(err, flag.ErrHelp) {
		return err
	}
	return usageError{err: err, reported: true}
}
