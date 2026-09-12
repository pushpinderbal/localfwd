# localfwd

`localfwd` watches an SSH host for listening TCP ports and forwards them to your
local machine. It is intended for remote development environments that do not
automatically forward newly started development servers.

It uses your installed OpenSSH client, so aliases, keys, agents, `ProxyJump`,
host-key checking, and other settings in `~/.ssh/config` continue to work.

## Setup

Install [mise](https://mise.jdx.dev/), then run:

```sh
mise trust
mise install
mise run test
mise run build
```

The binary is written to `bin/localfwd`. To install it on your path:

```sh
install -m 0755 bin/localfwd ~/.local/bin/localfwd
```

## Usage

Connect using the same destination you use with `ssh`:

```sh
localfwd devbox
```

After SSH authentication completes, `localfwd` opens its interactive terminal
interface and begins forwarding discovered ports.

Run this command on your **local machine**. Terminals opened inside a remote
development environment may themselves run on the remote host, so use a separate
local terminal unless you have confirmed where the shell is running.

### Controls

The status dot shows the state of each discovered port:

- Green: forwarded locally.
- Red: manually disabled.
- Gray: no longer listening on the remote host.

Click a port row to toggle its forward. You can also navigate with the arrow
keys (or `j`/`k`) and press Space or Enter. Press `q`, Escape, or Ctrl-C to quit.
Mouse interaction requires a terminal emulator with xterm-style mouse support.

Leave `localfwd` running during your remote development session; it scans every
two seconds. A forward becomes unavailable after the listener disappears from
three consecutive scans. Quitting closes the SSH connection and all forwards.

Useful options:

```sh
# Forward only common web-development ports.
localfwd --include 3000,4000,5173,8000-8999 devbox

# Keep forwards after their remote listeners stop.
localfwd --missing-scans 0 devbox

# Pass an OpenSSH option (repeat the flag for more than one).
localfwd --ssh-option ProxyJump=bastion devbox
```

Run `localfwd -h` for all options.

## Behavior and security

- Only unprivileged remote ports (1024 and above) are considered by default.
- Port 22 is excluded by default.
- Forwards bind to local loopback (`127.0.0.1`), not the LAN.
- The same local port is used when available. If it is occupied, `localfwd`
  chooses a free ephemeral port and prints the actual local address.
- The forwarding target follows the discovered listener, including IPv6. Wildcard
  IPv4 and IPv6 listeners are reached through their corresponding loopback address.
- Discovery requires `ss`, `netstat`, or `lsof` on the remote host.

`localfwd` creates a temporary OpenSSH control socket and removes it on exit. It
does not modify your SSH configuration.

## License

[MIT](LICENSE)
