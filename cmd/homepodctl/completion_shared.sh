# Shared Bash/Zsh argument walk. Metadata and candidate arrays are generated.
_homepodctl_contains() {
  local wanted="$1" item
  shift
  for item in "$@"; do
    [[ "$item" == "$wanted" ]] && return 0
  done
  return 1
}

_homepodctl_context() {
  options=() value_flags=() position_flags=() boolean_flags=() terminal=() children=()
  positional='' start=0 repeat=0 stop=0 legacy=0 literal=0
  case "$1" in
{{CONTEXTS}}
  esac
}

_homepodctl_value_group() {
  value_group='none'
  case "$1" in
{{VALUE_GROUPS}}
  esac
}

_homepodctl_boolean_word() {
  local word="$1"
  word="${word#"${word%%[!$' \t\r\n']*}"}"
  word="${word%"${word##*[!$' \t\r\n']}"}"
  case "$word" in
{{BOOLEAN_WORDS}}) return 0 ;;
  esac
  return 1
}

_homepodctl_scan() {
  local context=root pending='' optional=0 ended=0 position=0 offset=0 word flag value attached flag_legacy
  local positional start repeat stop legacy literal value_group
  local -a options value_flags position_flags boolean_flags terminal children
  group=none
  _homepodctl_context "$context"
  while (( $# > 1 )); do
    word="$1"
    shift
    if [[ -n "$pending" ]]; then
      pending=''
      continue
    fi
    if (( optional )); then
      optional=0
      _homepodctl_boolean_word "$word" && continue
    fi
    if (( ! ended )); then
      if [[ "$word" == -- ]]; then
        [[ "$context" == root ]] && return
        # A delimiter before plan's target belongs to the wrapper only.
        [[ "$context" != plan ]] && ended=1
        continue
      fi
      if [[ "$word" == -* && "$word" != - ]]; then
        flag="${word%%=*}"
        value='' attached=0 flag_legacy=$legacy
        if [[ "$word" == *=* ]]; then value="${word#*=}"; attached=1; fi
        [[ "$context" == plan* && "$flag" == --json ]] && flag_legacy=1
        if (( legacy )) && [[ "$flag" != --* && "$flag" != -h && "$flag" != -f ]]; then flag="-$flag"; fi
        [[ "$flag" == -f && "$word" != -f ]] && return
        _homepodctl_contains "$flag" "${options[@]}" || return
        if _homepodctl_contains "$flag" "${terminal[@]}"; then return; fi
        if _homepodctl_contains "$flag" "${value_flags[@]}"; then
          if _homepodctl_contains "$flag" "${position_flags[@]}"; then offset=1; fi
          if (( ! attached )) || { [[ -z "$value" ]] && (( ! flag_legacy )); }; then pending="$flag"; fi
        elif _homepodctl_contains "$flag" "${boolean_flags[@]}"; then
          if (( ! attached )) || { [[ -z "$value" ]] && (( ! flag_legacy )); }; then optional=1; fi
        elif (( attached )); then
          return
        fi
        continue
      fi
    fi
    if (( position == 0 )) && _homepodctl_contains "$word" "${children[@]}"; then
      if [[ "$context" == root ]]; then context="$word"; else context="$context $word"; fi
      _homepodctl_context "$context"
      continue
    fi
    if (( literal || stop )); then return; fi
    if (( ${#children[@]} > 0 )) && [[ -z "$positional" ]]; then return; fi
    position=$((position + 1))
  done
  current="$1"
  if [[ -n "$pending" ]]; then
    _homepodctl_value_group "$pending"
    group="$value_group"
  elif (( ! ended )) && [[ "$current" == -* ]]; then
    if [[ "$current" == *=* ]]; then
      flag="${current%%=*}"
      _homepodctl_contains "$flag" "${options[@]}" || return
      if _homepodctl_contains "$flag" "${value_flags[@]}" || _homepodctl_contains "$flag" "${boolean_flags[@]}"; then
        group="inline:$flag"
      fi
    else
      group="options:$context"
    fi
  elif (( position == 0 && ${#children[@]} > 0 )); then
    group="children:$context"
  elif [[ -n "$positional" ]] && (( position + offset >= start )) && (( repeat || position + offset == start )); then
    group="$positional"
  elif (( ! ended )) && [[ -z "$current" ]]; then
    group="options:$context"
  fi
}

_homepodctl_values() {
  candidates=() descriptions=()
  case "$1" in
{{CANDIDATES}}
  esac
}
