package tooltest

import (
	"strconv"
	"strings"
)

// GSPages returns the pages a gs run's args name, as a stand-in gs reads them: its -sPageList's,
// a page or a range of them, such as 3-7, between each comma, or its -dFirstPage alone, and none
// for a run that names neither.
func GSPages(args []string) []int {
	var pages []int
	for _, a := range args {
		if v, ok := strings.CutPrefix(a, "-dFirstPage="); ok {
			n, _ := strconv.Atoi(v)
			return []int{n}
		}
		if v, ok := strings.CutPrefix(a, "-sPageList="); ok {
			for f := range strings.SplitSeq(v, ",") {
				first, last, _ := strings.Cut(f, "-")
				from, _ := strconv.Atoi(first)
				to := from
				if last != "" {
					to, _ = strconv.Atoi(last)
				}
				for n := from; n <= to; n++ {
					pages = append(pages, n)
				}
			}
		}
	}
	return pages
}
