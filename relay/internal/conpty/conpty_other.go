//go:build !windows

package conpty

func spawn(executable string, args []string, cols, rows uint16) (PTY, error) {
	_ = executable
	_ = args
	_ = cols
	_ = rows
	return nil, ErrUnsupported
}
