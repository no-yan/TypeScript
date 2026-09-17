BEGIN {
	print "pair\torder\tsubtrees\tbaseline_ns_op\tcandidate_ns_op\tdelta_percent\tcandidate_faster"
}
{
	attempt = substr($1, 1, 2) + 0
	variant = index($1, "baseline") ? "baseline" : "candidate"
	split($2, parts, "/")
	size = parts[3]
	sub(/^subtrees-/, "", size)
	value[attempt, variant, size] = $3
}
END {
	for (pair = 1; pair <= 12; pair++) {
		first = pair * 2 - 1
		second = pair * 2
		order = ((first SUBSEP "baseline" SUBSEP "32") in value) ? "AB" : "BA"
		for (j = 1; j <= 3; j++) {
			size = (j == 1 ? 32 : (j == 2 ? 256 : 4096))
			baseline = ((first SUBSEP "baseline" SUBSEP size) in value) ? value[first, "baseline", size] : value[second, "baseline", size]
			candidate = ((first SUBSEP "candidate" SUBSEP size) in value) ? value[first, "candidate", size] : value[second, "candidate", size]
			delta = 100 * (candidate / baseline - 1)
			printf "%d\t%s\t%d\t%d\t%d\t%.3f\t%s\n", pair, order, size, baseline, candidate, delta, (candidate < baseline ? "yes" : "no")
		}
	}
}
