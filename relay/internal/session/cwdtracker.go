package session

import "strings"

// cwdTracker scans terminal output bytes for OSC 9;9 sequences emitted by
// the shell prompt and reports the current working directory.
//
// State is persisted across pump reads because OSC sequences may straddle
// a 32 KiB pump boundary. Recognized form:
//
//	ESC ] 9 ; 9 ; <path> BEL          (or ST = ESC \ )
//
// Surrounding double quotes around <path> are tolerated; pwsh-style
// emissions without quotes and Windows Terminal-style emissions with
// quotes both decode to the same string.
type cwdTracker struct {
	state cwdParserState
	body  []byte
}

type cwdParserState int

const (
	cwdScan cwdParserState = iota
	cwdAfterEsc
	cwdInOSC
	cwdOSCEsc
)

// cwdMaxBody bounds the per-OSC accumulation so a runaway sequence
// cannot grow memory without bound.
const cwdMaxBody = 4096

func newCWDTracker() *cwdTracker { return &cwdTracker{} }

// feed processes a chunk of raw output and returns each newly observed
// CWD path string. Most chunks return nil.
func (t *cwdTracker) feed(p []byte) []string {
	var out []string
	for _, b := range p {
		switch t.state {
		case cwdScan:
			if b == 0x1b {
				t.state = cwdAfterEsc
			}
		case cwdAfterEsc:
			switch b {
			case ']':
				t.state = cwdInOSC
				t.body = t.body[:0]
			case 0x1b:
				// stay in AfterEsc on consecutive ESCs
			default:
				t.state = cwdScan
			}
		case cwdInOSC:
			switch b {
			case 0x07:
				if path, ok := parseOSC99(t.body); ok {
					out = append(out, path)
				}
				t.state = cwdScan
				t.body = t.body[:0]
			case 0x1b:
				t.state = cwdOSCEsc
			default:
				if len(t.body) < cwdMaxBody {
					t.body = append(t.body, b)
				}
			}
		case cwdOSCEsc:
			if b == '\\' {
				if path, ok := parseOSC99(t.body); ok {
					out = append(out, path)
				}
				t.state = cwdScan
				t.body = t.body[:0]
			} else {
				t.body = t.body[:0]
				if b == 0x1b {
					t.state = cwdAfterEsc
				} else {
					t.state = cwdScan
				}
			}
		}
	}
	return out
}

func parseOSC99(body []byte) (string, bool) {
	const prefix = "9;9;"
	s := string(body)
	if !strings.HasPrefix(s, prefix) {
		return "", false
	}
	payload := strings.TrimSpace(s[len(prefix):])
	payload = strings.Trim(payload, `"`)
	if payload == "" {
		return "", false
	}
	return payload, true
}
