#!/usr/bin/env bash
set -euo pipefail
umask 077

state=/home/xubohan/.local/state/repomesh-b026
runtime_env="$state/runtime.env"
old_release=/home/xubohan/projects/Repomesh_Go_ver/dist/repomesh-0.2.0-b02-local-20260912-r1
new_release=/home/xubohan/projects/Repomesh_Go_ver/dist/repomesh-0.2.0-b026-utc-20260912-r2
auth=/home/xubohan/.config/repomesh/auth.json
expected_release_hash=5af0545707a9feb020436b56dfab407255a6b8d51046eeb4ed7f51bc41c76152
old_web_pid=177022
old_coordinator_pid=177023

validate_process() {
    local pid=$1
    local expected_exe=$2
    local uid exe
    test -d "/proc/$pid" || {
        printf 'refusing_switch: pid %s exited\n' "$pid" >&2
        exit 20
    }
    uid=$(awk '/^Uid:/{print $2}' "/proc/$pid/status")
    exe=$(readlink -f "/proc/$pid/exe")
    test "$uid" = 1000 && test "$exe" = "$expected_exe" || {
        printf 'refusing_switch: pid %s identity mismatch\n' "$pid" >&2
        exit 21
    }
}

validate_process "$old_web_pid" "$old_release/bin/repomesh-web"
validate_process "$old_coordinator_pid" "$old_release/bin/repomesh-coordinator"
test "$(sha256sum "$new_release/release.json" | awk '{print $1}')" = "$expected_release_hash"
test -f "$runtime_env" && test ! -L "$runtime_env"
test "$(stat -c '%u:%a:%F' "$runtime_env")" = "1000:600:regular file"
test -f "$auth" && test ! -L "$auth"
test "$(stat -c '%u:%a:%F' "$auth")" = "1000:600:regular file"
perl -ne '
    $all++ if /REPOMESH_RELEASE/;
    $exact++ if /^export REPOMESH_RELEASE=[^\r\n]*\n?$/;
    END { exit(($all == 1 && $exact == 1) ? 0 : 1) }
' "$runtime_env" || {
    printf 'refusing_switch: REPOMESH_RELEASE must be one exact export assignment\n' >&2
    exit 22
}

stamp=$(date +%Y%m%dT%H%M%S%z)
backup="$state/runtime.env.pre-r2-$stamp-$$.bak"
perl -MFcntl=:DEFAULT -e '
    my ($src,$dst)=@ARGV;
    sysopen(my $in,$src,O_RDONLY) or die "backup_source_open";
    sysopen(my $out,$dst,O_WRONLY|O_CREAT|O_EXCL,0600) or die "backup_target_create";
    while (1) {
        my $n=sysread($in,my $buf,65536);
        die "backup_read" unless defined $n;
        last if $n == 0;
        my $off=0;
        while ($off < $n) {
            my $w=syswrite($out,$buf,$n-$off,$off);
            die "backup_write" unless defined $w;
            $off += $w;
        }
    }
    close($out) or die "backup_close";
' "$runtime_env" "$backup"

next_env=$(mktemp "$state/runtime.env.next.XXXXXX")
perl -e '
    my ($src,$dst,$release)=@ARGV;
    open my $in,"<",$src or die "runtime_source_open";
    open my $out,">",$dst or die "runtime_target_open";
    my $seen=0;
    while (my $line=<$in>) {
        if ($line =~ /^export REPOMESH_RELEASE=[^\r\n]*\n?$/) {
            die "duplicate_REPOMESH_RELEASE" if $seen++;
            print {$out} "export REPOMESH_RELEASE=$release\n";
        } else {
            print {$out} $line;
        }
    }
    die "missing_REPOMESH_RELEASE" unless $seen == 1;
    close($out) or die "runtime_target_close";
' "$runtime_env" "$next_env" "$new_release"
perl -e '
    my ($old,$new)=@ARGV;
    open my $a,"<",$old or die; open my $b,"<",$new or die;
    local $/; my $x=<$a>; my $y=<$b>;
    $x =~ s/^export REPOMESH_RELEASE=[^\r\n]*\n?/export REPOMESH_RELEASE=<release>\n/m
        or die "old_release_line";
    $y =~ s/^export REPOMESH_RELEASE=[^\r\n]*\n?/export REPOMESH_RELEASE=<release>\n/m
        or die "new_release_line";
    die "runtime_other_bytes_changed" unless $x eq $y;
' "$runtime_env" "$next_env"
chmod 600 "$next_env"
mv "$next_env" "$runtime_env"

kill -TERM "$old_coordinator_pid" "$old_web_pid"
for pid in "$old_coordinator_pid" "$old_web_pid"; do
    for _ in $(seq 1 50); do
        test ! -d "/proc/$pid" && break
        sleep 0.2
    done
    test ! -d "/proc/$pid" || {
        printf 'old_process_did_not_stop pid=%s\n' "$pid" >&2
        exit 30
    }
done

set -a
. "$runtime_env"
set +a
test "$REPOMESH_RELEASE" = "$new_release"
test -n "${REPOMESH_DATABASE_URL:-}"

web_log="$state/web-r2-$stamp.log"
coordinator_log="$state/coordinator-r2-$stamp.log"
set -o noclobber
: > "$web_log"
: > "$coordinator_log"
set +o noclobber
chmod 600 "$web_log" "$coordinator_log"

setsid "$new_release/bin/repomesh-web" --addr 127.0.0.1:8443 --assets "$new_release/web/dist" --auth-config "$auth" >"$web_log" 2>&1 < /dev/null &
new_web_pid=$!
setsid "$new_release/bin/repomesh-coordinator" --auth-config "$auth" >"$coordinator_log" 2>&1 < /dev/null &
new_coordinator_pid=$!
sleep 2
kill -0 "$new_web_pid"
kill -0 "$new_coordinator_pid"
listener=$(ss -H -ltnp 'sport = :8443')
case "$listener" in
    *"pid=$new_web_pid,"*) ;;
    *) printf 'new_web_listener_mismatch\n' >&2; exit 40 ;;
esac

printf '%s\n' "$new_web_pid" > "$state/web.pid"
printf '%s\n' "$new_coordinator_pid" > "$state/coordinator.pid"
chmod 600 "$state/web.pid" "$state/coordinator.pid"

report=$(mktemp /tmp/repomesh-b026-r2-switch-redacted-XXXXXX.txt)
chmod 600 "$report"
{
    printf 'release_hash=%s\n' "$expected_release_hash"
    printf 'runtime_backup=%s\n' "$backup"
    printf 'old_web_pid=%s\nold_coordinator_pid=%s\n' "$old_web_pid" "$old_coordinator_pid"
    printf 'new_web_pid=%s\nnew_coordinator_pid=%s\n' "$new_web_pid" "$new_coordinator_pid"
    printf 'web_log=%s\ncoordinator_log=%s\n' "$web_log" "$coordinator_log"
    printf 'listener=127.0.0.1:8443\n'
} > "$report"
printf 'switch_complete=true report=%s\n' "$report"
