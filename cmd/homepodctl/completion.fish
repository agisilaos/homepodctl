# fish completion for homepodctl
function _homepodctl_scan
    set -l context root
    set -l pending ''
    set -l optional 0
    set -l ended 0
    set -l position 0
    set -l offset 0
    set -l current $argv[-1]
    set -e argv[-1]
    set -l boolean_words {{BOOLEAN_WORDS}}
    set -l options
    set -l value_flags
    set -l position_flags
    set -l boolean_flags
    set -l terminal
    set -l children
    set -l positional
    set -l start
    set -l repeat
    set -l stop
    set -l legacy
    set -l literal
    while true
        set options
        set value_flags
        set position_flags
        set boolean_flags
        set terminal
        set children
        set positional ''
        set start 0
        set repeat 0
        set stop 0
        set legacy 0
        set literal 0
        switch $context
{{CONTEXTS}}
        end
        if not set -q argv[1]
            break
        end
        set -l word $argv[1]
        set -e argv[1]
        if test -n "$pending"
            set pending ''
            continue
        end
        if test $optional = 1
            set optional 0
            if contains -- (string lower -- (string trim -- "$word")) $boolean_words
                continue
            end
        end
        if test $ended = 0
            if test "$word" = --
                test "$context" = root; and return
                if test "$context" != plan
                    set ended 1
                end
                continue
            end
            if string match -q -- '-*' "$word"; and test "$word" != -
                set -l parts (string split -m 1 = -- "$word")
                set -l flag $parts[1]
                set -l attached 0
                set -l value ''
                set -l flag_legacy $legacy
                if set -q parts[2]
                    set attached 1
                    set value $parts[2]
                end
                if string match -q 'plan*' "$context"; and test "$flag" = --json
                    set flag_legacy 1
                end
                if test $legacy = 1; and not string match -q -- '--*' "$flag"; and not contains -- "$flag" -h -f
                    set flag -$flag
                end
                if test "$flag" = -f; and test "$word" != -f
                    return
                end
                contains -- "$flag" $options; or return
                contains -- "$flag" $terminal; and return
                if contains -- "$flag" $value_flags
                    if contains -- "$flag" $position_flags
                        set offset 1
                    end
                    if test $attached = 0; or begin; test -z "$value"; and test $flag_legacy = 0; end
                        set pending $flag
                    end
                else if contains -- "$flag" $boolean_flags
                    if test $attached = 0; or begin; test -z "$value"; and test $flag_legacy = 0; end
                        set optional 1
                    end
                else if test $attached = 1
                    return
                end
                continue
            end
        end
        if test $position = 0; and contains -- "$word" $children
            if test "$context" = root
                set context $word
            else
                set context "$context $word"
            end
            continue
        end
        if test $literal = 1; or test $stop = 1
            return
        end
        if set -q children[1]; and test -z "$positional"
            return
        end
        set position (math $position + 1)
    end
    set -l group none
    if test -n "$pending"
        switch $pending
{{VALUE_GROUPS}}
        end
    else if test $ended = 0; and string match -q -- '-*' "$current"
        if string match -q '*=*' -- "$current"
            set -l flag (string split -m 1 = -- "$current")[1]
            contains -- "$flag" $options; or return
            if contains -- "$flag" $value_flags; or contains -- "$flag" $boolean_flags
                set group inline:$flag
            end
        else
            set group options:$context
        end
    else if test $position = 0; and set -q children[1]
        set group children:$context
    else if test -n "$positional"; and test (math $position + $offset) -ge $start; and begin; test $repeat = 1; or test (math $position + $offset) = $start; end
        set group $positional
    else if test $ended = 0; and test -z "$current"
        set group options:$context
    end
    printf '%s\n' "$group"
end

function _homepodctl_wants
    set -l line (commandline -cp | string collect --allow-empty)
    # All registrations for one completion share the same pure argument walk.
    if not set -q _homepodctl_cache_line; or test "$_homepodctl_cache_line" != "$line"
        set -l raw_current (commandline -ct | string collect --allow-empty)
        set -l prefix_length (math (string length -- "$line") - (string length -- "$raw_current"))
        set -l prefix (string sub -l $prefix_length -- "$line" | string collect --allow-empty)
        set -l tokens
        set -l current_tokens
        # Tokenize without evaluating substitutions; NUL framing preserves
        # quoted newlines in completed values.
        printf '%s\0' "$prefix" | read -z -t -a tokens
        printf '%s\0' "$raw_current" | read -z -t -a current_tokens
        set -l current ''
        if set -q current_tokens[1]
            set current $current_tokens[1]
        end
        set -e tokens[1]
        set -g _homepodctl_cache_group (_homepodctl_scan $tokens "$current")
        set -g _homepodctl_cache_current "$current"
        set -g _homepodctl_cache_line "$line"
    end
    test "$_homepodctl_cache_group" = "$argv[1]"
end

function _homepodctl_candidates
    # Fish otherwise applies fuzzy matching, which can offer unrelated
    # spellings for a prefix such as --c. All shells use literal prefixes.
    set -l pattern '^'(string escape --style=regex -- "$_homepodctl_cache_current")
    for candidate in $argv
        set -l name (string split -m 1 \t -- "$candidate")[1]
        if string match -q -r -- "$pattern" "$name"
            printf '%s\0' "$candidate"
        end
    end
end

complete -c homepodctl -f
{{REGISTRATIONS}}
