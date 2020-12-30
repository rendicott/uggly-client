package boxes

import (
	"bytes"
	"strings"
	"github.com/mitchellh/go-wordwrap"
)

// thanks to https://stackoverflow.com/users/639133/mozey
func splitSubN(s string, n int) []string {
    sub := ""
    subs := []string{}

    runes := bytes.Runes([]byte(s))
    l := len(runes)
    for i, r := range runes {
        sub = sub + string(r)
        if (i + 1) % n == 0 {
            subs = append(subs, sub)
            sub = ""
        } else if (i + 1) == l {
            subs = append(subs, sub)
        }
    }

    return subs
}

func wrap(s string, fillWidth int, hardBreaks bool) (map[int][]rune) {
	charMap := make(map[int][]rune)
	var splitS []string
	if hardBreaks {
		splitS = splitSubN(s, fillWidth)
	} else {
		sn := wordwrap.WrapString(s, uint(fillWidth))
		splitS = strings.Split(sn, "\n")
	}
    for i, line := range splitS {
        charMap[i] = make([]rune, len(line))
        for j, char := range(line) {
            charMap[i][j] = char
        }
    }
    return charMap
}

