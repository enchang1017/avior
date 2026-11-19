package api

import "strconv"

// Only browser clients opt in: machine clients retain JSON's numeric types.
func browserRows(rows [][]any) [][]any {
	out := make([][]any, len(rows))
	for i, row := range rows {
		out[i] = make([]any, len(row))
		for j, value := range row {
			out[i][j] = browserValue(value)
		}
	}
	return out
}

func browserValue(cell any) any {
	if value, ok := cell.(int64); ok {
		const safeInteger = 1<<53 - 1
		if value > safeInteger || value < -safeInteger {
			return strconv.FormatInt(value, 10)
		}
	}
	return cell
}
