package suggest

func Distance(left, right string) int {
	a := []rune(left)
	b := []rune(right)
	distance := make([][]int, len(a)+1)
	for row := range distance {
		distance[row] = make([]int, len(b)+1)
		distance[row][0] = row
	}
	for column := range distance[0] {
		distance[0][column] = column
	}
	for row := 1; row <= len(a); row++ {
		for column := 1; column <= len(b); column++ {
			cost := 0
			if a[row-1] != b[column-1] {
				cost = 1
			}
			distance[row][column] = min(
				distance[row][column-1]+1,
				distance[row-1][column]+1,
				distance[row-1][column-1]+cost,
			)
			if row > 1 &&
				column > 1 &&
				a[row-1] == b[column-2] &&
				a[row-2] == b[column-1] {
				distance[row][column] = min(
					distance[row][column],
					distance[row-2][column-2]+1,
				)
			}
		}
	}
	return distance[len(a)][len(b)]
}
