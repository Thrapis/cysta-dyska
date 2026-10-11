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
	"strings"
	"time"

	"cysta-dyska/internal/attribute"
	"cysta-dyska/internal/fastscan"
	"cysta-dyska/internal/scan"
	"cysta-dyska/internal/sources/locations"
	"cysta-dyska/internal/sources/registry"
)

func main() {
	if fastscan.IsHelperCall(os.Args) { // started by -fast through UAC
		os.Exit(fastscan.RunHelper(os.Args[2:]))
	}
	top := flag.Int("top", 20, "number of largest entries to print")
	workers := flag.Int("workers", 0, "directories read in parallel (0 = default)")
	quiet := flag.Bool("q", false, "no live progress on stderr")
	fast := flag.Bool("fast", false, "read the drive's MFT through an elevated helper (UAC prompt; drive roots only)")
	culprits := flag.Bool("culprits", false, "explain what uses the space (rule pack + installed apps)")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: cysta-scan [-top N] [-workers N] [-q] [-culprits] <folder>")
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
	var tree *scan.Node
	var err error
	if *fast {
		var steps []fastscan.Step
		tree, steps, err = fastscan.RunTimed(ctx, strings.TrimSuffix(flag.Arg(0), `\`))
		for _, s := range steps {
			fmt.Fprintf(os.Stderr, "%8.2fs  %s\n", s.Took.Seconds(), s.Name)
		}
		if tree != nil {
			last = scan.Progress{Files: tree.Files, Dirs: tree.Dirs, Bytes: tree.Alloc}
		}
	} else {
		tree, err = scan.Scan(ctx, flag.Arg(0), scan.Options{
			Workers: w,
			OnProgress: func(p scan.Progress) {
				last = p
				if !*quiet {
					fmt.Fprintf(os.Stderr, "\r%d files, %s", p.Files, human(p.Bytes))
				}
			},
		})
	}
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

	if *culprits {
		printCulprits(tree, *top)
	}
}

func printCulprits(tree *scan.Node, top int) {
	start := time.Now()
	pack, err := attribute.LoadEmbedded()
	if err != nil {
		fmt.Fprintln(os.Stderr, "rules:", err)
		os.Exit(1)
	}
	apps, err := registry.Installed()
	if err != nil {
		fmt.Fprintln(os.Stderr, "registry:", err)
	}
	found := attribute.Match(tree, pack, locations.Resolve(os.Getenv), apps)

	var total int64
	for _, f := range found {
		total += f.Alloc
	}
	fmt.Printf("\nculprits: %d findings, %s explained (%.0f%% of scan) from %d apps in %s\n\n",
		len(found), human(total), 100*float64(total)/float64(max(tree.Alloc, 1)), len(apps),
		time.Since(start).Round(time.Millisecond))
	for i, f := range found {
		if i == top {
			break
		}
		size := human(f.Alloc)
		if f.Unreadable() {
			size = "unknown"
		}
		fmt.Printf("%10s  %-6s  %-14s %-22s %s\n", size, f.Rule.Risk, f.Rule.Category, f.Owner, f.Node.Path())
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
