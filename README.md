# trawl

A fast, keyboard-driven, dual-pane terminal SFTP file manager. Connect to a host,
browse local and remote side by side, and copy files or whole directories with a
single keystroke. One static binary, secure defaults, no daemon.

## Install

`trawl` ships as a single static binary — no runtime, no dependencies.

### Linux (one-liner)

```sh
curl -fsSL https://raw.githubusercontent.com/liam-od/trawl/main/install.sh | sh
```

This downloads the latest release into `/usr/local/bin` (using `sudo` if needed). To install
somewhere on your own `PATH` without `sudo`:

```sh
curl -fsSL https://raw.githubusercontent.com/liam-od/trawl/main/install.sh | BIN_DIR="$HOME/.local/bin" sh
```

### Manual download

Grab the binary for your platform from the
[latest release](https://github.com/liam-od/trawl/releases/latest):

| Platform      | File                        |
|---------------|-----------------------------|
| Linux (amd64) | `trawl-linux-amd64`         |
| Windows (amd64) | `trawl-windows-amd64.exe` |

On Linux, mark it executable and move it onto your `PATH`:

```sh
chmod +x trawl-linux-amd64
sudo mv trawl-linux-amd64 /usr/local/bin/trawl
```

On Windows, drop `trawl-windows-amd64.exe` somewhere on your `PATH` (rename to `trawl.exe` if you
like) and run it from a terminal.

### From source

Requires Go 1.26 (the result is a static, CGO-free binary):

```sh
go install github.com/liam-od/trawl/cmd/trawl@latest    # → $GOBIN
```

or clone and build:

```sh
git clone https://github.com/liam-od/trawl
cd trawl
make build            # → bin/trawl
make build-all        # → bin/trawl-linux-amd64, bin/trawl-windows-amd64.exe
```

## Usage

```sh
trawl [flags] user@host[:port][:/remote/path]
trawl [flags] <saved-host-name>
```

The argument is either a live target, or — if it contains no `@` — the name of a
host you saved with `trawl --setup` (see below).

Examples:

```sh
trawl me@example.com                 # start in your remote home directory
trawl me@example.com:2222:/var/www   # custom port and starting path
trawl prod                           # connect to the saved host "prod"
```

On the first connection to a host you'll be shown its key fingerprint and asked
whether to add it to `~/.ssh/known_hosts`. After that, a changed host key is
refused automatically.

### Setup and saved connections

`trawl --setup` opens an interactive menu, written to
`~/.config/trawl/config.json`:

```
1) Edit global defaults     default user, key path, port, password fallback
2) Add a saved host         name, user@host, port, key, remote dir, local dir
3) Edit a saved host
4) Remove a saved host
5) Quit
```

The global defaults let you drop repetitive flags (`trawl host` instead of
`trawl --user me --key … me@host`). A **saved host** goes further: it remembers a
whole connection under a name, so `trawl prod` connects to it. Each saved host can
also set a default **local** and **remote** directory, so the two panels open
where you want them — a leading `~` expands to your local home for the local
directory and to the remote account's home for the remote directory.

```sh
trawl --setup     # choose "2", name it "prod", set its dirs
trawl prod        # connects; panels open in the saved directories
```

Settings resolve as: command-line flag > saved host > global default in
`~/.config/trawl/config.json` > built-in default.

### Authentication

The SSH agent is tried first, then a key file (`--key`, or `key_path` from setup),
then an interactive password prompt. To use a specific key:

```sh
ssh-add ~/.ssh/id_ed25519      # via the agent (recommended), or…
trawl --key ~/.ssh/id_ed25519 me@host
```

### Keys

| Key | Action |
|-----|--------|
| `Tab` | switch active panel |
| `↑` / `↓` (or `k` / `j`) | move the cursor |
| `Enter` | open directory |
| `Backspace` (or `h`) | parent directory |
| `F5` / `c` | copy the selected file or directory to the other panel |
| `F7` | show / hide the transfer queue |
| `r` | refresh the active panel |
| `q` (or `F10`, `Ctrl+C`) | quit |

Copies run one at a time through the transfer queue, which shows progress and the
current transfer rate. Press `F5`/`c` again while one is running to queue more.

### Flags

```
--port N          SSH port (overrides :port in the target; default 22)
--user NAME       override the user in the target
--key PATH        private key file (otherwise the SSH agent is used)
--password        allow password authentication as a fallback (default true)
--no-password     disable the password fallback
--known-hosts P   known_hosts file (default ~/.ssh/known_hosts)
--config PATH     config file (default ~/.config/trawl/config.json)
--setup           manage saved hosts and global defaults, then exit
--version         print version and exit
--help            show this help and exit
```
