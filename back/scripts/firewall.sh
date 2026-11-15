#!/usr/bin/env bash
# Межсетевой экран ConfHub: сегменты docker-сетей + nftables.
#
#   firewall.sh apply                 применить политику из firewall/policy.conf
#   firewall.sh show                  показать действующие правила и счётчики (вывод nft)
#   firewall.sh rules                 те же правила одной строкой на правило
#   firewall.sh test [--summary]      проверить разрешённые и запрещённые соединения
#   firewall.sh revoke <src> <dst>    снять разрешающее правило (демонстрация отказа)
#   firewall.sh restore               вернуть полную политику
#   firewall.sh down                  удалить таблицы экрана
#
# Трафик между контейнерами одного сегмента коммутируется мостом и не проходит
# через iptables, поэтому фильтрация выполняется в семействе bridge. Выход во
# внешние сети маршрутизируется и фильтруется в семействе ip.
set -euo pipefail
cd "$(dirname "$0")/.."

PROJECT="${COMPOSE_PROJECT:-confhub}"
POLICY="${FIREWALL_POLICY:-firewall/policy.conf}"
STATE_DIR="${FIREWALL_STATE_DIR:-/run/confhub-firewall}"
REVOKED="${STATE_DIR}/${PROJECT}.revoked"
TABLE="${PROJECT//-/_}_fw"
SEGMENTS=(edge app data broker storage)

[[ "$PROJECT" =~ ^[a-zA-Z0-9_-]+$ ]] || { echo "Unsafe compose project" >&2; exit 2; }
[[ "$(id -u)" == 0 ]] || { echo "Run as root" >&2; exit 2; }
command -v nft >/dev/null || { echo "Install nftables" >&2; exit 2; }
[[ -f "$POLICY" ]] || { echo "Policy file not found: $POLICY" >&2; exit 2; }
mkdir -p "$STATE_DIR"

declare -A GROUPS_MAP=() ADDR=() PID=() BRIDGE=() SUBNET=()

load_topology() {
    local segment network line service net ip pid name
    for segment in "${SEGMENTS[@]}"; do
        network="${PROJECT}_${segment}"
        BRIDGE[$segment]="$(docker network inspect -f '{{index .Options "com.docker.network.bridge.name"}}' "$network")"
        SUBNET[$segment]="$(docker network inspect -f '{{(index .IPAM.Config 0).Subnet}}' "$network")"
        [[ -n "${BRIDGE[$segment]}" && -n "${SUBNET[$segment]}" ]] || { echo "Network $network is not segmented" >&2; exit 1; }
    done
    while read -r line; do
        [[ -n "$line" ]] || continue
        service="${line%% *}"
        pid="$(cut -d' ' -f2 <<<"$line")"
        PID[$service]="$pid"
        for net in $(cut -d' ' -f3- <<<"$line"); do
            segment="${net%%=*}"; segment="${segment#"${PROJECT}_"}"
            ip="${net#*=}"
            # A container that is restarting has no address yet and gets no rules.
            if [[ "$ip" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then ADDR[$segment/$service]="$ip"; fi
        done
    done < <(docker ps -q --filter status=running --filter "label=com.docker.compose.project=${PROJECT}" | xargs -r docker inspect -f \
        '{{index .Config.Labels "com.docker.compose.service"}} {{.State.Pid}}{{range $k,$v := .NetworkSettings.Networks}} {{$k}}={{$v.IPAddress}}{{end}}')
    (( ${#PID[@]} )) || { echo "No running containers in project $PROJECT" >&2; exit 1; }
    # Containers of other projects attached to a segment (for example Prometheus)
    # are addressed in the policy by container name.
    for segment in "${SEGMENTS[@]}"; do
        while read -r name ip; do
            [[ -n "$name" && "$name" != "${PROJECT}-"* ]] || continue
            ADDR[$segment/$name]="${ip%/*}"
            PID[$name]="$(docker inspect -f '{{.State.Pid}}' "$name")"
        done < <(docker network inspect -f '{{range .Containers}}{{.Name}} {{.IPv4Address}}{{"\n"}}{{end}}' "${PROJECT}_${segment}")
    done
}

# expand <segment> <name|@group>  ->  "service ip" lines; absent group members are skipped
expand() {
    local segment="$1" name="$2" member members
    if [[ "$name" == @* ]]; then
        members="${GROUPS_MAP[${name#@}]:-}"
        [[ -n "$members" ]] || { echo "Unknown group $name" >&2; exit 1; }
        for member in $members; do
            [[ -n "${ADDR[$segment/$member]:-}" ]] && echo "$member ${ADDR[$segment/$member]}"
        done
    else
        # A service that is not running gets no rules; apply reports it.
        if [[ -n "${ADDR[$segment/$name]:-}" ]]; then echo "$name ${ADDR[$segment/$name]}"; fi
    fi
    return 0
}

# Fills ALLOW (segment src srcip dst dstip proto ports) and EGRESS (src srcip proto ports).
# REVOKED_RULES (segment src srcip dst dstip) are allow rules withdrawn by `revoke`.
ALLOW=()
EGRESS=()
REVOKED_RULES=()
declare -A MISSING=()
note_missing() {
    local segment="$1" name
    shift
    for name in "$@"; do
        if [[ "$name" != @* && -z "${ADDR[$segment/$name]:-}" ]]; then MISSING[$name]=1; fi
    done
}
load_policy() {
    local kind a b c d e src dst sname sip dname dip
    while read -r kind a b c d e; do
        case "$kind" in
            ''|'#'*) ;;
            group) GROUPS_MAP[$a]="$b $c $d $e" ;;
            allow)
                [[ " ${SEGMENTS[*]} " == *" $a "* ]] || { echo "Unknown segment $a" >&2; exit 1; }
                [[ "$d" =~ ^(tcp|udp)$ && "$e" =~ ^[0-9,-]+$ ]] || { echo "Bad rule: $kind $a $b $c $d $e" >&2; exit 1; }
                note_missing "$a" "$b" "$c"
                while read -r sname sip; do
                    [[ -n "$sname" ]] || continue
                    while read -r dname dip; do
                        [[ -n "$dname" && "$sname" != "$dname" ]] || continue
                        if [[ -f "$REVOKED" ]] && grep -qxF "$sname $dname" "$REVOKED"; then
                            REVOKED_RULES+=("$a $sname $sip $dname $dip")
                            continue
                        fi
                        ALLOW+=("$a $sname $sip $dname $dip $d $e")
                    done < <(expand "$a" "$c")
                done < <(expand "$a" "$b")
                ;;
            egress)
                [[ "$b" =~ ^(tcp|udp)$ && "$c" =~ ^[0-9,-]+$ ]] || { echo "Bad rule: $kind $a $b $c" >&2; exit 1; }
                note_missing app "$a"
                while read -r sname sip; do
                    [[ -n "$sname" ]] && EGRESS+=("$sname $sip $b $c")
                done < <(expand app "$a")
                ;;
            *) echo "Unknown directive: $kind" >&2; exit 1 ;;
        esac
    done < <(sed 's/[[:space:]]#.*$//' "$POLICY")
}

render() {
    local rule segment sname sip dname dip proto ports bridges subnets
    bridges="$(printf '"%s", ' "${BRIDGE[@]}")"; bridges="${bridges%, }"
    subnets="$(printf '%s, ' "${SUBNET[@]}")"; subnets="${subnets%, }"
    cat <<EOF
table bridge ${TABLE}
delete table bridge ${TABLE}
table ip ${TABLE}
delete table ip ${TABLE}

# Трафик между контейнерами внутри сегментов (коммутируется мостом).
table bridge ${TABLE} {
    chain forward {
        type filter hook forward priority filter; policy accept;
        meta ibrname { ${bridges} } jump segments
    }
    chain segments {
        ether type arp accept
EOF
    # A withdrawn rule also cuts connections that are already open.
    for rule in "${REVOKED_RULES[@]}"; do
        read -r segment sname sip dname dip <<<"$rule"
        echo "        ip saddr ${sip} ip daddr ${dip} counter drop comment \"revoked: ${sname} -> ${dname}\""
    done
    cat <<EOF
        ct state established,related accept
        ct state invalid drop
EOF
    for rule in "${ALLOW[@]}"; do
        read -r segment sname sip dname dip proto ports <<<"$rule"
        echo "        ip saddr ${sip} ip daddr ${dip} ${proto} dport { ${ports} } counter accept comment \"${segment}: ${sname} -> ${dname}\""
    done
    cat <<EOF
        limit rate 10/minute log prefix "confhub-fw drop: "
        counter drop comment "default deny"
    }
}

# Выход из сегментов во внешние сети (маршрутизируется хостом).
table ip ${TABLE} {
    set segments {
        type ipv4_addr; flags interval
        elements = { ${subnets} }
    }
    chain forward {
        type filter hook forward priority -10; policy accept;
        ip saddr != @segments accept
        ip daddr @segments accept
        ct state established,related accept
EOF
    for rule in "${EGRESS[@]}"; do
        read -r sname sip proto ports <<<"$rule"
        echo "        ip saddr ${sip} ${proto} dport { ${ports} } counter accept comment \"egress: ${sname}\""
    done
    cat <<EOF
        limit rate 10/minute log prefix "confhub-fw egress drop: "
        counter drop comment "default deny egress"
    }
}
EOF
}

apply() {
    load_topology
    load_policy
    local ruleset
    ruleset="$(mktemp "${STATE_DIR}/ruleset.XXXXXX")"
    render > "$ruleset"
    nft -c -f "$ruleset"
    nft -f "$ruleset"
    rm -f -- "$ruleset"
    echo "Firewall applied to project ${PROJECT}: ${#ALLOW[@]} allow rules, ${#EGRESS[@]} egress rules, default deny."
    if (( ${#MISSING[@]} )); then echo "Not running, rules skipped: ${!MISSING[*]}"; fi
    if [[ -s "$REVOKED" ]]; then
        echo "Revoked (dropped, including open connections):"
        awk '{ print "  " $1 " -> " $2 }' "$REVOKED"
    fi
}

# probe <service> <ip> <port>: TCP connect from the container's network namespace.
probe() {
    nsenter -t "${PID[$1]}" -n timeout 2 bash -c "exec 3<>/dev/tcp/$2/$3" 2>/dev/null
}

# Checks run in parallel; each writes one line to the report file.
check() {
    local expect="$1" segment="$2" sname="$3" dname="$4" dip="$5" port="$6"
    (
        got=blocked; mark=ok
        if probe "$sname" "$dip" "$port"; then got=open; fi
        [[ "$got" == "$expect" ]] || mark=FAIL
        printf '  %-4s %-8s %-16s -> %-16s :%-5s expected %-7s got %s\n' \
            "$mark" "$segment" "$sname" "$dname" "$port" "$expect" "$got" >> "$report"
    ) &
    jobs_started=$((jobs_started + 1))
    if (( jobs_started % 60 == 0 )); then wait; fi
}

# Callers must `wait` for the probes first: inside a pipeline this function runs
# in a subshell, where `wait` would not see the background jobs.
print_report() {
    sort -k2,2 -k3,3 -k5,5 "$report"
    : > "$report"
}

# One line per rule, built from the ruleset loaded in the kernel (not from policy.conf).
list_rules() {
    { nft list table bridge "$TABLE"; nft list table ip "$TABLE"; } | python3 -c '
import re, sys
allow = egress = 0
for line in sys.stdin:
    line = line.strip()
    if line.startswith("table "):
        family = line.split()[1]
        title = "inside segments" if family == "bridge" else "from segments to external networks"
        print("%s (%s)" % (line.rstrip(" {"), title))
        continue
    m = re.search(r"counter packets (\d+) bytes \d+ (accept|drop) comment \"([^\"]*)\"", line)
    if not m:
        continue
    packets, verdict, comment = m.groups()
    port = re.search(r"(tcp|udp) dport (\{[^}]*\}|\d+)", line)
    port = "%s %s" % (port.group(1), port.group(2).strip("{} ").replace(" ", "")) if port else ""
    if verdict == "accept":
        if comment.startswith("egress:"):
            egress += 1
        else:
            allow += 1
    print("  %-6s %-38s %-14s pkts %s" % (verdict, comment, port, packets))
print("Total: %d allow rules inside segments, %d egress rules, default deny." % (allow, egress))
'
}

run_test() {
    local brief="${1:-}"
    load_topology
    load_policy
    local rule segment sname sip dname dip proto ports port key jobs_started=0 report summary failures
    report="$(mktemp "${STATE_DIR}/report.XXXXXX")"
    summary="${report}.all"
    declare -A allowed=() seen=()
    [[ -n "$brief" ]] || echo "Allowed by policy (must connect):"
    for rule in "${ALLOW[@]}"; do
        read -r segment sname sip dname dip proto ports <<<"$rule"
        [[ "$proto" == tcp ]] || continue
        for port in ${ports//,/ }; do
            allowed[$sname/$dip/$port]=1
            check open "$segment" "$sname" "$dname" "$dip" "$port"
        done
    done
    wait
    if [[ -n "$brief" ]]; then print_report > "$summary"; else print_report | tee "$summary"; fi
    [[ -n "$brief" ]] || { echo; echo "Not in policy (must be dropped):"; }
    # Every listening target is also probed from every neighbour that has no rule for it.
    for rule in "${ALLOW[@]}"; do
        read -r segment _ _ dname dip proto ports <<<"$rule"
        [[ "$proto" == tcp ]] || continue
        for port in ${ports//,/ }; do
            [[ -z "${seen[$segment/$dname/$port]:-}" ]] || continue
            seen[$segment/$dname/$port]=1
            for key in "${!ADDR[@]}"; do
                [[ "$key" == "$segment/"* ]] || continue
                sname="${key#*/}"
                [[ "$sname" != "$dname" && -z "${allowed[$sname/$dip/$port]:-}" ]] || continue
                check blocked "$segment" "$sname" "$dname" "$dip" "$port"
            done
        done
    done
    wait
    if [[ -n "$brief" ]]; then print_report >> "$summary"; else print_report | tee -a "$summary"; fi
    failures="$(grep -c '^  FAIL' "$summary" || true)"
    if [[ -n "$brief" ]]; then
        # Per segment: totals and one sample connection of each kind.
        awk '
            { seg = $2; if (!(seg in seen)) { seen[seg] = 1; order[++n] = seg } }
            $8 == "open"    { open[seg]++;    if ($1 == "ok") openok[seg]++;    if (!(seg in so)) so[seg] = $3 " -> " $5 " " $6 }
            $8 == "blocked" { blocked[seg]++; if ($1 == "ok") blockedok[seg]++; if (!(seg in sb)) sb[seg] = $3 " -> " $5 " " $6 }
            $1 == "FAIL"    { fails[++f] = $0 }
            END {
                printf "  %-8s %-22s %-22s\n", "segment", "allowed: connected", "forbidden: dropped"
                for (i = 1; i <= n; i++) {
                    seg = order[i]
                    printf "  %-8s %3d of %-15d %3d of %-15d\n", seg, openok[seg], open[seg], blockedok[seg], blocked[seg]
                    to += open[seg]; tb += blocked[seg]; tok += openok[seg]; tbk += blockedok[seg]
                }
                printf "  %-8s %3d of %-15d %3d of %-15d\n", "total", tok, to, tbk, tb
                print ""
                print "Samples:"
                for (i = 1; i <= n; i++) {
                    seg = order[i]
                    printf "  %-8s open     %s\n", seg, (seg in so) ? so[seg] : "nothing to check"
                    printf "  %-8s blocked  %s\n", seg, (seg in sb) ? sb[seg] : "nothing to check: every pair is allowed"
                }
                for (i = 1; i <= f; i++) print fails[i]
            }' "$summary"
    fi
    echo
    echo "Checked $(wc -l < "$summary") connections."
    rm -f -- "$report" "$summary"
    if (( failures )); then echo "FAILED: $failures check(s)"; return 1; fi
    echo "All checks passed."
}

case "${1:-}" in
    apply) apply ;;
    show)
        nft list table bridge "$TABLE"
        nft list table ip "$TABLE"
        ;;
    rules) list_rules ;;
    test)
        [[ -z "${2:-}" || "${2:-}" == --summary ]] || { echo "Usage: firewall.sh test [--summary]" >&2; exit 2; }
        run_test "${2:-}"
        ;;
    revoke)
        [[ $# -eq 3 ]] || { echo "Usage: firewall.sh revoke <src-service> <dst-service>" >&2; exit 2; }
        echo "$2 $3" >> "$REVOKED"
        apply
        ;;
    restore)
        rm -f -- "$REVOKED"
        apply
        ;;
    down)
        nft delete table bridge "$TABLE" 2>/dev/null || true
        nft delete table ip "$TABLE" 2>/dev/null || true
        rm -f -- "$REVOKED"
        echo "Firewall tables removed for project ${PROJECT}."
        ;;
    *) sed -n '2,10p' "$0" | sed 's/^# \{0,1\}//' >&2; exit 2 ;;
esac
