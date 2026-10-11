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
	"runtime"
	"time"

	"cysta-dyska/internal/scan"
)

func main() {
	top := flag.Int("top", 20, "number of largest entries to print")
	workers := flag.Int("workers", 0, "directories read in parallel (0 = default)")
	quiet := flag.Bool("q", false, "no live progress on stderr")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: cysta-scan [-top N] [-workers N] [-q] <folder>")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	w := *workers
	if w <= 0 {
		w = scan.DefaultWorkers()
	}
	start := time.Now()
	var last scan.Progress
	tree, err := scan.Scan(ctx, flag.Arg(0), scan.Options{
		Workers: w,
		OnProgress: func(p scan.Progress) {
			last = p
			if !*quiet {
				fmt.Fprintf(os.Stderr, "\r%d files, %s", p.Files, human(p.Bytes))
			}
		},
	})
	elapsed := time.Since(start)
	if !*quiet {
		fmt.Fprintln(os.Stderr)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	fmt.Printf("%s\n  on disk %s, logical %s\n  %d files, %d dirs, %d unreadable\n  %s with %d workers, heap %s\n\n",
		tree.Path(), human(tree.Alloc), human(tree.Size), tree.Files, tree.Dirs, last.Errors,
		elapsed.Round(time.Millisecond), w, human(int64(ms.HeapAlloc)))
	for i, c := range tree.Children {
		if i == *top {
			break
		}
		kind := " "
		switch {
		case c.Link:
			kind = " -> (link, not followed)"
		case c.IsDir:
			kind = "/"
		}
		fmt.Printf("%10s  %10s  %s%s\n", human(c.Alloc), human(c.Size), c.Name, kind)
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
