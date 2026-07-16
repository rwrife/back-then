package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// shellSnippets holds the sourceable integration snippet for each supported
// shell. Each defines a `bt-cd` function (find + fzf-select + cd into the
// chosen file's folder) and a short `bt` alias. Snippets are emitted verbatim
// as static strings; no user or remote data is interpolated or eval'd.
var shellSnippets = map[string]string{
	"bash": btBashZsh,
	"zsh":  btBashZsh,
	"fish": btFish,
}

const btBashZsh = `# back-then shell integration
# Add to your ~/.bashrc or ~/.zshrc:
#   eval "$(back-then shell-init bash)"   # or: zsh
alias bt='back-then'

# bt-cd "<time phrase>": fuzzy-pick a result and cd into its folder.
bt-cd() {
  if ! command -v fzf >/dev/null 2>&1; then
    echo "bt-cd: fzf is required (https://github.com/junegunn/fzf)" >&2
    return 1
  fi
  local sel
  sel=$(back-then find "$1" --print0 | fzf --read0 --prompt="back-then> ") || return 1
  [ -n "$sel" ] || return 1
  cd "$(dirname "$sel")" || return 1
}
`

const btFish = `# back-then shell integration
# Add to your ~/.config/fish/config.fish:
#   back-then shell-init fish | source
alias bt='back-then'

function bt-cd --description 'fuzzy-pick a back-then result and cd into its folder'
  if not command -v fzf >/dev/null 2>&1
    echo "bt-cd: fzf is required (https://github.com/junegunn/fzf)" >&2
    return 1
  end
  set -l sel (back-then find "$argv[1]" --print0 | fzf --read0 --prompt="back-then> ")
  or return 1
  test -n "$sel"; or return 1
  cd (dirname "$sel")
end
`

// newShellInitCmd returns `back-then shell-init [bash|zsh|fish]`, which prints a
// sourceable snippet wiring up a `bt` alias and a `bt-cd` fuzzy-jump helper.
func newShellInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "shell-init [bash|zsh|fish]",
		Short: "Print a sourceable shell snippet (bt alias + fuzzy bt-cd helper)",
		Long: `Print a shell snippet you can source to add back-then helpers.

The snippet defines a short "bt" alias and a "bt-cd" function that runs a
find, lets you fuzzy-pick a result with fzf, and cd's into the chosen file's
folder. It is emitted as a static string; nothing is eval'd from remote input.

Install it by adding one line to your shell rc file:

  bash:  eval "$(back-then shell-init bash)"   # ~/.bashrc
  zsh:   eval "$(back-then shell-init zsh)"    # ~/.zshrc
  fish:  back-then shell-init fish | source    # ~/.config/fish/config.fish`,
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"bash", "zsh", "fish"},
		RunE: func(cmd *cobra.Command, args []string) error {
			snippet, ok := shellSnippets[args[0]]
			if !ok {
				return fmt.Errorf("unsupported shell %q: choose bash, zsh, or fish", args[0])
			}
			_, err := io.WriteString(cmd.OutOrStdout(), snippet)
			return err
		},
	}
}
