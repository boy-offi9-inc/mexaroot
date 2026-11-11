package main

import (
	"errors"
	"flag"
	"fmt"
	"strings"
)

var completionShells = []string{"bash", "zsh", "fish"}

func completionCommand(args []string, fs *flag.FlagSet) (int, error) {
	if len(args) != 1 {
		return 2, errors.New("usage: mexaroot completion <bash|zsh|fish>")
	}
	script, err := completionScript(args[0], fs)
	if err != nil {
		return 2, err
	}
	fmt.Print(script)
	return 0, nil
}

func completionScript(shell string, fs *flag.FlagSet) (string, error) {
	switch shell {
	case "bash":
		return bashCompletion(fs), nil
	case "zsh":
		return zshCompletion(fs), nil
	case "fish":
		return fishCompletion(fs), nil
	default:
		return "", fmt.Errorf("unsupported shell %q (use bash, zsh, or fish)", shell)
	}
}

func isBoolFlag(f *flag.Flag) bool {
	boolean, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && boolean.IsBoolFlag()
}

func flagWords(fs *flag.FlagSet) []string {
	var words []string
	fs.VisitAll(func(f *flag.Flag) {
		words = append(words, "-"+f.Name)
	})
	return words
}

func bashCompletion(fs *flag.FlagSet) string {
	return fmt.Sprintf(`_mexaroot() {
  local cur prev
  cur="${COMP_WORDS[COMP_CWORD]}"
  prev="${COMP_WORDS[COMP_CWORD-1]}"
  case "$prev" in
    exec|shell)
      COMPREPLY=($(compgen -W "$(mexaroot runtimes 2>/dev/null | cut -f1)" -- "$cur"))
      return
      ;;
    completion)
      COMPREPLY=($(compgen -W "%s" -- "$cur"))
      return
      ;;
  esac
  if [[ "$cur" == -* ]]; then
    COMPREPLY=($(compgen -W "%s" -- "$cur"))
  else
    COMPREPLY=($(compgen -W "%s" -- "$cur"))
  fi
}
complete -F _mexaroot mexaroot
`, strings.Join(completionShells, " "), strings.Join(flagWords(fs), " "), strings.Join(commandNames(), " "))
}

func zshEscape(text string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`'`, `'\''`,
		`:`, `\:`,
		`[`, `\[`,
		`]`, `\]`,
	)
	return replacer.Replace(text)
}

func zshCompletion(fs *flag.FlagSet) string {
	var commandLines []string
	for _, command := range commands {
		commandLines = append(commandLines, fmt.Sprintf("    '%s:%s'", command.name, zshEscape(command.summary)))
	}

	var flagLines []string
	fs.VisitAll(func(f *flag.Flag) {
		spec := fmt.Sprintf("-%s[%s]", f.Name, zshEscape(f.Usage))
		if !isBoolFlag(f) {
			spec += ":value:"
		}
		flagLines = append(flagLines, fmt.Sprintf("    '%s' \\", strings.ReplaceAll(spec, "'", `'\''`)))
	})

	return fmt.Sprintf(`#compdef mexaroot

_mexaroot() {
  local -a commands
  commands=(
%s
  )

  _arguments -C \
%s
    '1:command:->command' \
    '*::argument:->argument'

  case $state in
    command)
      _describe 'command' commands
      ;;
    argument)
      case $words[1] in
        exec|shell)
          if (( CURRENT == 2 )); then
            local -a runtimes
            runtimes=(${(f)"$(mexaroot runtimes 2>/dev/null | cut -f1)"})
            _describe 'runtime' runtimes
          fi
          ;;
        completion)
          _values 'shell' %s
          ;;
      esac
      ;;
  esac
}

compdef _mexaroot mexaroot
`, strings.Join(commandLines, "\n"), strings.Join(flagLines, "\n"), strings.Join(completionShells, " "))
}

func fishEscape(text string) string {
	return strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(text)
}

func fishCompletion(fs *flag.FlagSet) string {
	var b strings.Builder
	b.WriteString("complete -c mexaroot -f\n")
	for _, command := range commands {
		fmt.Fprintf(&b, "complete -c mexaroot -n '__fish_use_subcommand' -a '%s' -d '%s'\n", command.name, fishEscape(command.summary))
	}
	b.WriteString("complete -c mexaroot -n '__fish_seen_subcommand_from exec shell' -a '(mexaroot runtimes 2>/dev/null | cut -f1)'\n")
	fmt.Fprintf(&b, "complete -c mexaroot -n '__fish_seen_subcommand_from completion' -a '%s'\n", strings.Join(completionShells, " "))
	fs.VisitAll(func(f *flag.Flag) {
		required := ""
		if !isBoolFlag(f) {
			required = " -r"
		}
		fmt.Fprintf(&b, "complete -c mexaroot -o %s -d '%s'%s\n", f.Name, fishEscape(f.Usage), required)
	})
	return b.String()
}
