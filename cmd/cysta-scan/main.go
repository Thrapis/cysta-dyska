// Command cysta-scan scans a folder and prints its largest entries.
// It shares the scanner with the desktop app and is used for debugging and
// benchmarking without the UI.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

	"cysta-dyska/internal/scan"
)

func main() {
	top := flag.Int("top", 20, "number of largest entries to print")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: cysta-scan [-top N] <folder>")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	start := time.Now()
	var last scan.Progress
	tree, err := scan.Scan(ctx, flag.Arg(0), func(p scan.Progress) {
		last = p
		fmt.Fprintf(os.Stderr, "\r%d files, %s", p.Files, human(p.Bytes))
	})
	fmt.Fprintln(os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	fmt.Printf("%s  %s  (%d files, %d dirs, %d unreadable) in %s\n\n",
		tree.Path, human(tree.Size), tree.Files, tree.Dirs, last.Errors,
		time.Since(start).Round(time.Millisecond))
	for i, c := range tree.Children {
		if i == *top {
			break
		}
		kind := " "
		if c.IsDir {
			kind = "/"
		}
		fmt.Printf("%10s  %s%s\n", human(c.Size), c.Name, kind)
	}
}

func human(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
