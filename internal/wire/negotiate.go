package wire

var SupportedVersions = []int{Version}

func Negotiate(client, supported []int) (int, bool) {
	best := 0
	for _, c := range client {
		for _, s := range supported {
			if c == s && c > best {
				best = c
			}
		}
	}
	return best, best > 0
}
