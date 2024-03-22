package plugin

import (
	"bufio"
	"embed"
	"fmt"
	"strings"
)

//go:embed golden
var embedFS embed.FS

// This whole thing needs some work. It's a bit of a mess, but it works. Turn this into a proper
// state machine or something.

const marker = `// +++`

func prepareGoldenClient() (string, error) {
	f, err := embedFS.Open("golden/golden_client.go")
	if err != nil {
		return "", err
	}
	sc := bufio.NewScanner(f)
	var b strings.Builder
	var ok, okBlock bool
	for sc.Scan() {
		if strings.HasPrefix(strings.TrimSpace(sc.Text()), marker) {
			switch annotation := strings.TrimPrefix(strings.TrimSpace(sc.Text()), marker); annotation {
			case "BEGIN TEMPLATE":
				ok = true
				continue
			case "BEGIN BLOCK":
				okBlock = true
				continue
			case "END BLOCK":
				okBlock = false
				continue
			case "END TEMPLATE":
				ok = false
				continue
			default:
				return "", fmt.Errorf("unexpected expression: %q", annotation)
			}
		}
		if ok {
			if okBlock {
				txt := strings.TrimPrefix(strings.TrimSpace(sc.Text()), "// ")
				if txt != "//" {
					b.WriteString(txt)
					b.WriteString("\n")
				}
			} else {
				b.WriteString(sc.Text())
				b.WriteString("\n")
			}
		}
	}
	return b.String(), nil
}
