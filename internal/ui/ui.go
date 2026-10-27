package ui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

const reset = "\x1b[0m"

type UI struct {
	in    *bufio.Reader
	out   io.Writer
	err   io.Writer
	color bool
	yes   bool
}

func New(in io.Reader, out, errOut io.Writer, color, yes bool) *UI {
	return &UI{
		in:    bufio.NewReader(in),
		out:   out,
		err:   errOut,
		color: color,
		yes:   yes,
	}
}

func SupportsColor(w io.Writer, noColor bool) bool {
	if noColor || os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func (u *UI) Okf(format string, args ...any) {
	u.prefixed(u.out, "92", "[+]", format, args...)
}

func (u *UI) Warnf(format string, args ...any) {
	u.prefixed(u.err, "93", "[!]", format, args...)
}

func (u *UI) Errorf(format string, args ...any) {
	u.prefixed(u.err, "91", "[x]", format, args...)
}

func (u *UI) Infof(format string, args ...any) {
	u.prefixed(u.out, "96", "[i]", format, args...)
}

func (u *UI) Printf(format string, args ...any) {
	fmt.Fprintf(u.out, format, args...)
}

func (u *UI) Bold(text string) string {
	return u.paint("1", text)
}

func (u *UI) Cyan(text string) string {
	return u.paint("96", text)
}

func (u *UI) Green(text string) string {
	return u.paint("92", text)
}

func (u *UI) Yellow(text string) string {
	return u.paint("93", text)
}

func (u *UI) Confirm(question string) (bool, error) {
	if u.yes {
		return true, nil
	}
	fmt.Fprintf(u.out, "%s [y/N] ", u.Bold(question))
	line, err := u.in.ReadString('\n')
	if err != nil && line == "" {
		if err == io.EOF {
			return false, nil
		}
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

func (u *UI) Ask(prompt string) (string, error) {
	fmt.Fprint(u.out, prompt)
	line, err := u.in.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func (u *UI) prefixed(w io.Writer, code, prefix, format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	fmt.Fprintf(w, "%s %s\n", u.paint(code, prefix), message)
}

func (u *UI) paint(code, text string) string {
	if !u.color {
		return text
	}
	return "\x1b[" + code + "m" + text + reset
}
