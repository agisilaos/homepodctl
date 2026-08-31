package main

import (
	_ "embed"
	"fmt"
	"strings"
)

//go:embed completion_shared.sh
var sharedCompletionShell string

//go:embed completion.fish
var fishCompletionShell string

func boolNumber(value bool) int {
	if value {
		return 1
	}
	return 0
}

func renderCompletionShell(v completionVocabulary, quote func([]string) string) string {
	var contexts, groups, boolPatterns, candidates strings.Builder
	for _, c := range v.contexts {
		fmt.Fprintf(&contexts, "    %s)\n      options=(%s)\n      value_flags=(%s)\n      position_flags=(%s)\n      boolean_flags=(%s)\n      terminal=(%s)\n      children=(%s)\n      positional=%s start=%d repeat=%d stop=%d legacy=%d literal=%d ;;\n",
			quote([]string{c.name}), quote(c.options), quote(c.values), quote(c.positionFlags), quote(c.booleans), quote(c.terminal), quote(c.children),
			quote([]string{c.positional}), c.start, boolNumber(c.repeat), boolNumber(c.stop), boolNumber(c.legacy), boolNumber(c.literal))
	}
	for _, flag := range sortedKeys(completionValueGroups) {
		fmt.Fprintf(&groups, "    %s) value_group=%s ;;\n", quote([]string{flag}), quote([]string{completionValueGroups[flag]}))
	}
	for i, word := range sortedKeys(booleanWords) {
		if i > 0 {
			boolPatterns.WriteByte('|')
		}
		for _, char := range word {
			if char >= 'a' && char <= 'z' {
				fmt.Fprintf(&boolPatterns, "[%c%c]", char, char-'a'+'A')
			} else {
				boolPatterns.WriteRune(char)
			}
		}
	}
	for _, name := range sortedKeys(v.groups) {
		values := v.groups[name]
		descriptions := completionGroupDescriptions(name, values)
		fmt.Fprintf(&candidates, "    %s) candidates=(%s); descriptions=(%s) ;;\n", quote([]string{name}), quote(values), quote(descriptions))
	}
	return strings.NewReplacer("{{CONTEXTS}}", contexts.String(), "{{VALUE_GROUPS}}", groups.String(),
		"{{BOOLEAN_WORDS}}", boolPatterns.String(), "{{CANDIDATES}}", candidates.String()).Replace(sharedCompletionShell)
}

func completionGroupDescriptions(group string, values []string) []string {
	if !strings.HasPrefix(group, "options:") && !strings.HasPrefix(group, "children:") && group != "commands" {
		return nil
	}
	descriptions := make([]string, len(values))
	for i, value := range values {
		descriptions[i] = completionDescriptions[value]
	}
	return descriptions
}

func renderBashCompletion(values completionValues) string {
	return "# bash completion for homepodctl\n" + renderCompletionShell(newCompletionVocabulary(values), bashArrayLiteral) + `
_homepodctl_unquote() {
  local raw="$1" quote='' char next
  unquoted=''
  # Remove shell quoting without evaluating user text or substitutions.
  while [[ -n "$raw" ]]; do
    char="${raw:0:1}"; raw="${raw:1}"
    if [[ "$quote" == "'" ]]; then
      if [[ "$char" == "'" ]]; then quote=''; else unquoted+="$char"; fi
    elif [[ "$char" == \\ ]]; then
      next="${raw:0:1}"
      if [[ -z "$quote" || "$next" == '$' || "$next" == '"' || "$next" == \\ || "$next" == $'\x60' || "$next" == $'\n' ]]; then
        [[ "$next" != $'\n' ]] && unquoted+="$next"
        raw="${raw:1}"
      else
        unquoted+="$char"
      fi
    elif [[ "$char" == "$quote" ]]; then
      quote=''
    elif [[ -z "$quote" && ( "$char" == "'" || "$char" == '"' ) ]]; then
      quote="$char"
    else
      unquoted+="$char"
    fi
  done
}

_homepodctl_completion() {
  local group current candidate index=1 split_prefix='' word unquoted join_next=0 adjacent remaining
  local -a candidates descriptions input decoded
  COMPREPLY=()
  remaining="${COMP_LINE#*"${COMP_WORDS[0]}"}"
  # Bash normally makes '=' a separate COMP_WORDS entry. Rejoin it for
  # scanning, then return only the part that Readline will replace.
  while (( index <= COMP_CWORD )); do
    word="${COMP_WORDS[index]}"
    adjacent=0
    if [[ -z "${COMP_LINE+x}" || "$remaining" == "$word"* ]]; then adjacent=1; fi
    remaining="${remaining#*"$word"}"
    if [[ "$word" == = && ${#input[@]} -gt 0 ]] && (( adjacent )); then
      input[${#input[@]}-1]+='='
      if (( index == COMP_CWORD )); then split_prefix="${input[${#input[@]}-1]}"; fi
      join_next=1
    elif (( join_next && adjacent )); then
      if (( index == COMP_CWORD )); then split_prefix="${input[${#input[@]}-1]}"; fi
      input[${#input[@]}-1]+="$word"
      join_next=0
    else
      input+=("$word")
      join_next=0
    fi
    index=$((index + 1))
  done
  for word in "${input[@]}"; do
    _homepodctl_unquote "$word"
    decoded+=("$unquoted")
  done
  _homepodctl_unquote "$split_prefix"
  split_prefix="$unquoted"
  _homepodctl_scan "${decoded[@]}"
  _homepodctl_values "$group"
  for candidate in "${candidates[@]}"; do
    if [[ "$candidate" == "$current"* ]]; then COMPREPLY+=("${candidate#"$split_prefix"}"); fi
  done
}
complete -F _homepodctl_completion homepodctl
`
}

func renderZshCompletion(values completionValues) string {
	return "#compdef homepodctl\n" + renderCompletionShell(newCompletionVocabulary(values), zshArrayLiteral) + `
_homepodctl() {
  emulate -L zsh
  local group current candidate word index=1
  local -a candidates descriptions matches match_descriptions input
  for word in "${words[@]:1:$((CURRENT-1))}"; do input+=("${(Q)word}"); done
  # PREFIX has quote removal applied even for an unfinished current quote.
  if (( ${+PREFIX} )); then input[-1]="$PREFIX"; fi
  _homepodctl_scan "${input[@]}"
  _homepodctl_values "$group"
  for candidate in "${candidates[@]}"; do
    if [[ "$candidate" == "$current"* ]]; then
      matches+=("$candidate")
      match_descriptions+=("${descriptions[index]}")
    fi
    index=$((index + 1))
  done
  # Keep values opaque: compadd receives array elements, never shell code.
  if (( ${#descriptions[@]} > 0 )); then
    compadd -d match_descriptions -a matches
  else
    compadd -a matches
  fi
}
_homepodctl "$@"
`
}

func fishArrayLiteral(values []string) string {
	literals := make([]string, 0, len(values))
	for _, value := range values {
		literals = append(literals, fishStringLiteral(value))
	}
	return strings.Join(literals, " ")
}

func renderFishCompletion(values completionValues) string {
	v := newCompletionVocabulary(values)
	var contexts, valueGroups, registrations strings.Builder
	for _, c := range v.contexts {
		fmt.Fprintf(&contexts, "        case %s\n            set options %s\n            set value_flags %s\n            set position_flags %s\n            set boolean_flags %s\n            set terminal %s\n            set children %s\n            set positional %s\n            set start %d\n            set repeat %d\n            set stop %d\n            set legacy %d\n            set literal %d\n",
			fishStringLiteral(c.name), fishArrayLiteral(c.options), fishArrayLiteral(c.values), fishArrayLiteral(c.positionFlags), fishArrayLiteral(c.booleans), fishArrayLiteral(c.terminal), fishArrayLiteral(c.children),
			fishStringLiteral(c.positional), c.start, boolNumber(c.repeat), boolNumber(c.stop), boolNumber(c.legacy), boolNumber(c.literal))
	}
	for _, flag := range sortedKeys(completionValueGroups) {
		fmt.Fprintf(&valueGroups, "            case %s\n                set group %s\n", fishStringLiteral(flag), fishStringLiteral(completionValueGroups[flag]))
	}
	for _, name := range sortedKeys(v.groups) {
		candidates := append([]string(nil), v.groups[name]...)
		for i, description := range completionGroupDescriptions(name, candidates) {
			if description != "" {
				candidates[i] += "\t" + description
			}
		}
		appendFishCompletion(&registrations, "_homepodctl_wants "+fishStringLiteral(name), candidates)
	}
	return strings.NewReplacer("{{CONTEXTS}}", strings.ReplaceAll(contexts.String(), " \n", "\n"), "{{VALUE_GROUPS}}", valueGroups.String(),
		"{{BOOLEAN_WORDS}}", fishArrayLiteral(sortedKeys(booleanWords)), "{{REGISTRATIONS}}", strings.TrimSuffix(registrations.String(), "\n")).Replace(fishCompletionShell)
}
