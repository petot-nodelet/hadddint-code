package web

import "fmt"

// FormatIDR renders cents as Indonesian Rupiah, e.g. 15000000 -> "Rp150.000".
func FormatIDR(cents int64) string {
	rupiah := cents / 100
	s := fmt.Sprintf("%d", rupiah)
	// group digits with dots from the right
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, '.')
		}
		out = append(out, c)
	}
	return "Rp" + string(out)
}
