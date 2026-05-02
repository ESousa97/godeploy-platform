// Command coveragefloor reads `go tool cover -func` output from stdin and exits
// non-zero if total coverage is below the minimum percent (first arg, default 30).
package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func main() {
	minPct := 30.0
	if len(os.Args) > 1 {
		if v, err := strconv.ParseFloat(os.Args[1], 64); err == nil && v > 0 {
			minPct = v
		}
	}
	sc := bufio.NewScanner(os.Stdin)
	var pct float64
	var found bool
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "total:") {
			continue
		}
		fields := strings.Fields(line)
		for _, f := range fields {
			if !strings.HasSuffix(f, "%") {
				continue
			}
			v := strings.TrimSuffix(f, "%")
			var err error
			pct, err = strconv.ParseFloat(v, 64)
			if err != nil {
				fmt.Fprintf(os.Stderr, "coveragefloor: parse %q: %v\n", f, err)
				os.Exit(1)
			}
			found = true
			break
		}
		break
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "coveragefloor: %v\n", err)
		os.Exit(1)
	}
	if !found {
		fmt.Fprintln(os.Stderr, "coveragefloor: no total line in cover -func output")
		os.Exit(1)
	}
	if pct < minPct {
		fmt.Fprintf(os.Stderr, "coveragefloor: %.1f%% is below minimum %.1f%%\n", pct, minPct)
		os.Exit(1)
	}
	fmt.Printf("coveragefloor: %.1f%% (minimum %.1f%%)\n", pct, minPct)
}
