package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
)

// Confirm asks a yes/no question on stdin and returns true only on an
// explicit y / yes. It returns false when stdin is not a terminal —
// there is nothing to prompt, so a scripted or piped run never blocks on
// a hidden question (callers gate destructive actions behind a --yes
// flag for those). The single owner of orbit's interactive y/N prompt.
func Confirm(prompt string) bool {
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return false
	}
	fmt.Print(prompt + " [y/N]: ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	return ans == "y" || ans == "yes"
}
